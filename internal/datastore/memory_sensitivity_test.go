package datastore

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Memory sensitivity: defaults, the manager's list filter and bulk patch, the SQL gate a restricted
// chat's reads go through, export/import, fold/undo and link behaviour.

const (
	sPublic    = models.MemorySensitivityPublic
	sPersonal  = models.MemorySensitivityPersonal
	sSensitive = models.MemorySensitivitySensitive
)

func sensitivityOf(t *testing.T, ds *Datastore, id uuid.UUID) models.MemorySensitivity {
	t.Helper()
	row, err := ds.dbClient.Memory.Get(context.Background(), id)
	require.NoError(t, err)
	return models.MemorySensitivity(row.Sensitivity)
}

func setSensitivity(t *testing.T, ds *Datastore, id uuid.UUID, level models.MemorySensitivity) {
	t.Helper()
	require.NoError(t, ds.dbClient.Memory.UpdateOneID(id).SetSensitivity(entmemory.Sensitivity(level)).Exec(context.Background()))
}

func createGlobalMemory(t *testing.T, ds *Datastore, userID uuid.UUID, content string, level models.MemorySensitivity) *models.Memory {
	t.Helper()
	mem, err := ds.CreateMemoryFromInput(context.Background(), userID, models.CreateMemoryInput{
		Content: content, Level: models.MemoryLevelGlobal, Sensitivity: level,
	})
	require.NoError(t, err)
	return mem
}

func TestMemorySensitivity_CreateDefaultsAndValidation(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)

	def := createGlobalMemory(t, ds, userID, "default", "")
	require.Equal(t, sPersonal, def.Sensitivity, "an unspecified level is personal, never public")
	require.Equal(t, sPublic, createGlobalMemory(t, ds, userID, "pub", sPublic).Sensitivity)
	require.Equal(t, sSensitive, createGlobalMemory(t, ds, userID, "sens", sSensitive).Sensitivity)

	_, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{
		Content: "bad", Level: models.MemoryLevelGlobal, Sensitivity: "secret",
	})
	require.ErrorIs(t, err, ErrInvalidRequestBody)

	// The agent-side create path takes the level too, and defaults it.
	created, err := ds.CreateMemory(ctx, userID, models.Memory{Content: "tool made", Scope: "User", Sensitivity: sSensitive}, nil, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, entmemory.SensitivitySensitive, created.Sensitivity)
	created, err = ds.CreateMemory(ctx, userID, models.Memory{Content: "tool made 2", Scope: "User"}, nil, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, entmemory.SensitivityPersonal, created.Sensitivity)
}

func TestMemorySensitivity_UpdateAndSummaryExcluded(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	mem := createGlobalMemory(t, ds, userID, "taxes", "")
	level := sSensitive
	updated, err := ds.UpdateMemory(ctx, userID, mem.ID, models.MemoryPatch{Sensitivity: &level})
	require.NoError(t, err)
	require.Equal(t, sSensitive, updated.Sensitivity)
	require.Equal(t, sSensitive, sensitivityOf(t, ds, mem.ID))

	// A patch that does not mention sensitivity leaves it alone.
	starred := true
	updated, err = ds.UpdateMemory(ctx, userID, mem.ID, models.MemoryPatch{Starred: &starred})
	require.NoError(t, err)
	require.Equal(t, sSensitive, updated.Sensitivity)

	bogus := models.MemorySensitivity("bogus")
	_, err = ds.UpdateMemory(ctx, userID, mem.ID, models.MemoryPatch{Sensitivity: &bogus})
	require.ErrorIs(t, err, ErrInvalidRequestBody)

	// Summary memories are thread-management state: they carry no sensitivity.
	summary, err := ds.dbClient.Memory.Create().SetContent("summary").SetScope(entmemory.ScopeSummary).
		SetOwnerID(userID).SetChatID(chatID).Save(ctx)
	require.NoError(t, err)
	_, err = ds.UpdateMemory(ctx, userID, summary.ID, models.MemoryPatch{Sensitivity: &level})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestMemorySensitivity_ListFilter(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	for _, c := range []struct {
		content string
		level   models.MemorySensitivity
	}{{"pub1", sPublic}, {"pub2", sPublic}, {"per1", sPersonal}, {"sens1", sSensitive}} {
		createGlobalMemory(t, ds, userID, c.content, c.level)
	}

	contents := func(f models.MemoryFilters) []string {
		res, err := ds.ListMemories(ctx, userID, 1, 50, f)
		require.NoError(t, err)
		var out []string
		for _, r := range res.Results {
			out = append(out, r.(*models.Memory).Content)
		}
		return out
	}
	exact := func(l models.MemorySensitivity) *models.MemorySensitivity { return &l }

	require.ElementsMatch(t, []string{"pub1", "pub2", "per1", "sens1"}, contents(models.MemoryFilters{}))
	require.ElementsMatch(t, []string{"pub1", "pub2"}, contents(models.MemoryFilters{Sensitivity: exact(sPublic)}))
	require.ElementsMatch(t, []string{"sens1"}, contents(models.MemoryFilters{Sensitivity: exact(sSensitive)}))
}

func TestMemoryIDsWithinSensitivity_OwnerScopedAndGated(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	owner, other := uuid.New(), uuid.New()
	createTestUser(t, ds, owner)
	createTestUser(t, ds, other)
	pub := createGlobalMemory(t, ds, owner, "pub", sPublic)
	per := createGlobalMemory(t, ds, owner, "per", sPersonal)
	sens := createGlobalMemory(t, ds, owner, "sens", sSensitive)
	foreign := createGlobalMemory(t, ds, other, "foreign", sPublic)
	ids := []uuid.UUID{pub.ID, per.ID, sens.ID, foreign.ID, uuid.New()}

	got, err := ds.MemoryIDsWithinSensitivity(ctx, owner, ids, sPersonal)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Contains(t, got, pub.ID)
	require.Contains(t, got, per.ID)

	got, err = ds.MemoryIDsWithinSensitivity(ctx, owner, ids, "")
	require.NoError(t, err)
	require.Len(t, got, 3, "an unrestricted limit still never returns another user's memory")

	got, err = ds.MemoryIDsWithinSensitivity(ctx, owner, nil, sPublic)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestPatchMemoriesBatch_SensitivityBulk(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	owner, other := uuid.New(), uuid.New()
	createTestUser(t, ds, owner)
	createTestUser(t, ds, other)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, owner)

	a := createGlobalMemory(t, ds, owner, "a", "")
	b := createGlobalMemory(t, ds, owner, "b", "")
	c := createGlobalMemory(t, ds, owner, "c", "")
	theirs := createGlobalMemory(t, ds, other, "theirs", "")
	summary, err := ds.dbClient.Memory.Create().SetContent("summary").SetScope(entmemory.ScopeSummary).
		SetOwnerID(owner).SetChatID(chatID).Save(ctx)
	require.NoError(t, err)

	level := sSensitive
	patch := models.MemoryPatch{Sensitivity: &level}

	// Partial mode: foreign and Summary ids are skipped like missing ones, the rest are set.
	res, err := ds.PatchMemoriesBatch(ctx, owner, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{a.ID, theirs.ID, summary.ID, b.ID}, Patch: patch,
	})
	require.NoError(t, err)
	require.Equal(t, 2, res.UpdatedCount)
	require.Equal(t, []uuid.UUID{a.ID, b.ID}, []uuid.UUID{res.Results[0].ID, res.Results[1].ID}, "results keep request order")
	for _, r := range res.Results {
		require.Equal(t, sSensitive, r.Sensitivity)
	}
	require.Equal(t, sSensitive, sensitivityOf(t, ds, a.ID))
	require.Equal(t, sSensitive, sensitivityOf(t, ds, b.ID))
	require.Equal(t, sPersonal, sensitivityOf(t, ds, c.ID), "ids not named are untouched")
	require.Equal(t, sPersonal, sensitivityOf(t, ds, theirs.ID), "another user's memory is never modified")
	require.Equal(t, entmemory.SensitivityPersonal, mustGetMemory(t, ds, summary.ID).Sensitivity, "Summary scope is excluded")

	// AllOrNone: one foreign id rejects the whole batch and changes nothing.
	pub := sPublic
	_, err = ds.PatchMemoriesBatch(ctx, owner, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{c.ID, theirs.ID}, Patch: models.MemoryPatch{Sensitivity: &pub}, AllOrNone: true,
	})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Equal(t, sPersonal, sensitivityOf(t, ds, c.ID))
	// ... and so does a Summary id.
	_, err = ds.PatchMemoriesBatch(ctx, owner, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{c.ID, summary.ID}, Patch: models.MemoryPatch{Sensitivity: &pub}, AllOrNone: true,
	})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Equal(t, sPersonal, sensitivityOf(t, ds, c.ID))

	// Duplicate ids in the request are fine.
	res, err = ds.PatchMemoriesBatch(ctx, owner, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{c.ID, c.ID}, Patch: models.MemoryPatch{Sensitivity: &pub}, AllOrNone: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.UpdatedCount)
	require.Equal(t, sPublic, sensitivityOf(t, ds, c.ID))

	// An invalid level is a request error.
	bogus := models.MemorySensitivity("bogus")
	_, err = ds.PatchMemoriesBatch(ctx, owner, models.BatchPatchMemoryInput{IDs: []uuid.UUID{a.ID}, Patch: models.MemoryPatch{Sensitivity: &bogus}})
	require.ErrorIs(t, err, ErrInvalidRequestBody)

	// A mixed patch still works through the per-id path and applies sensitivity too.
	starred := true
	res, err = ds.PatchMemoriesBatch(ctx, owner, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{a.ID, b.ID}, Patch: models.MemoryPatch{Sensitivity: &pub, Starred: &starred},
	})
	require.NoError(t, err)
	require.Equal(t, 2, res.UpdatedCount)
	require.Equal(t, sPublic, sensitivityOf(t, ds, a.ID))
	require.True(t, mustGetMemory(t, ds, a.ID).Starred)
}

func mustGetMemory(t *testing.T, ds *Datastore, id uuid.UUID) *ent.Memory {
	t.Helper()
	row, err := ds.dbClient.Memory.Get(context.Background(), id)
	require.NoError(t, err)
	return row
}

func TestMemorySensitivity_ExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	// Memory ids are primary keys, so the import target is a second database (another instance).
	dsDst, cleanupDst := newMemoryTestDatastore(t)
	defer cleanupDst()
	src, dst := uuid.New(), uuid.New()
	createTestUser(t, ds, src)
	createTestUser(t, dsDst, dst)

	pub := createGlobalMemory(t, ds, src, "export public", sPublic)
	sens := createGlobalMemory(t, ds, src, "export sensitive", sSensitive)
	per := createGlobalMemory(t, ds, src, "export personal", "")

	var buf bytes.Buffer
	require.NoError(t, ds.ExportMemories(ctx, src, &buf))
	records, err := ds.ExportMemoryRecords(ctx, src)
	require.NoError(t, err)
	byID := map[uuid.UUID]models.MemorySensitivity{}
	for _, r := range records {
		byID[r.ID] = r.Sensitivity
	}
	require.Equal(t, sPublic, byID[pub.ID])
	require.Equal(t, sSensitive, byID[sens.ID])
	require.Equal(t, sPersonal, byID[per.ID])

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	// Import into another account: the level is restored.
	_, err = dsDst.ImportMemories(ctx, dst, zr, func(context.Context, string) ([]float32, error) { return []float32{1, 2, 3}, nil })
	require.NoError(t, err)
	rows, err := dsDst.dbClient.Memory.Query().Where(entmemory.HasOwnerWith(user.ID(dst))).All(ctx)
	require.NoError(t, err)
	got := map[string]models.MemorySensitivity{}
	for _, r := range rows {
		got[r.Content] = models.MemorySensitivity(r.Sensitivity)
	}
	require.Equal(t, map[string]models.MemorySensitivity{
		"export public": sPublic, "export sensitive": sSensitive, "export personal": sPersonal,
	}, got)
}

func TestMemorySensitivity_ImportOldExportDefaultsToPersonalAndGarbageFailsClosed(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	// An export written before sensitivity existed has no such key.
	line := `{"id":"` + id.String() + `","content":"legacy memory","created_at":"` + now.Format(time.RFC3339) + `"}`
	zr := buildZipReaderForTest(t, map[string]string{"user.json": line})
	_, err := ds.ImportMemories(ctx, userID, zr, func(context.Context, string) ([]float32, error) { return []float32{1}, nil })
	require.NoError(t, err)
	require.Equal(t, sPersonal, sensitivityOf(t, ds, id))

	// A non-empty value that is not a level (hand-edited, corrupt, or the wrong case) fails
	// closed: the memory imports as sensitive rather than failing the row or reading as less
	// delicate than it may be.
	for _, garbage := range []string{"top-secret", "Public", "PERSONAL"} {
		id2 := uuid.New()
		line2 := strings.Replace(line, id.String(), id2.String(), 1)
		line2 = strings.Replace(line2, `"content"`, `"sensitivity":"`+garbage+`","content"`, 1)
		zr = buildZipReaderForTest(t, map[string]string{"user.json": line2})
		_, err = ds.ImportMemories(ctx, userID, zr, func(context.Context, string) ([]float32, error) { return []float32{1}, nil })
		require.NoError(t, err)
		require.Equal(t, sSensitive, sensitivityOf(t, ds, id2), "imported sensitivity %q", garbage)
	}
}

// --- merge: a survivor takes the most restricted level of its group; undo puts it back ---

func TestPersistMemoryMergeGroup_FoldTakesMostRestrictedAndUndoRestores(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	survivor := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Runs on weekends", confidence: 0.6, embedding: foldTestVector(1)})
	absorbedSens := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Training after the surgery", confidence: 0.6, embedding: foldTestVector(2)})
	absorbedPub := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Likes running", confidence: 0.6, embedding: foldTestVector(3)})
	setSensitivity(t, ds, survivor, sPublic)
	setSensitivity(t, ds, absorbedSens, sSensitive)
	setSensitivity(t, ds, absorbedPub, sPublic)

	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Runs on weekends"), 3,
		&survivor, []uuid.UUID{absorbedSens, absorbedPub}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, sSensitive, sensitivityOf(t, ds, survivor), "the survivor stands for a group containing sensitive material")
	require.Equal(t, sSensitive, sensitivityOf(t, ds, absorbedSens), "absorbed rows are retired, not relabelled")
	require.Equal(t, sPublic, sensitivityOf(t, ds, absorbedPub))

	event := foldTestEvent(t, ds, userID, survivor)
	require.Equal(t, "public", event.Snapshot.PriorSensitivity)
	require.Equal(t, "sensitive", event.Snapshot.FoldedSensitivity)

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)
	require.Equal(t, sPublic, sensitivityOf(t, ds, survivor), "undo restores the prior level")
}

func TestUndoFold_KeepsSensitivityTheUserChangedSinceTheFold(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	survivor := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Has a dog", confidence: 0.6})
	absorbed := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Dog is on medication", confidence: 0.6})
	setSensitivity(t, ds, absorbed, sSensitive)

	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Has a dog"), 2, &survivor, []uuid.UUID{absorbed}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, sSensitive, sensitivityOf(t, ds, survivor))

	// The user relabels the survivor afterwards; undoing the fold must not stomp that.
	setSensitivity(t, ds, survivor, sPublic)
	event := foldTestEvent(t, ds, userID, survivor)
	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)
	require.Equal(t, sPublic, sensitivityOf(t, ds, survivor))
}

func TestPersistMemoryMergeGroup_NewMemberSensitivity(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	// New-only group: created at the (already capped) level it was given.
	mem, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("A brand new fact"), 1, nil, nil, foldTestVector(1), uuid.Nil, nil, nil,
		WithNewMemberSensitivity(sSensitive))
	require.NoError(t, err)
	require.Equal(t, sSensitive, sensitivityOf(t, ds, mem.ID))

	// No option: the default, personal.
	mem, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Another new fact"), 1, nil, nil, foldTestVector(2), uuid.Nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, sPersonal, sensitivityOf(t, ds, mem.ID))

	// Folding a new sensitive member into a public survivor raises the survivor.
	survivor := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Has a cat", confidence: 0.6})
	setSensitivity(t, ds, survivor, sPublic)
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Has a cat"), 2, &survivor, nil, nil, uuid.Nil, nil, nil,
		WithNewMemberSensitivity(sSensitive))
	require.NoError(t, err)
	require.Equal(t, sSensitive, sensitivityOf(t, ds, survivor))

	// A new member at the unclassified default (personal, as extraction gives in an ordinary chat)
	// does not pull a survivor the user marked public back to personal.
	survivorPub := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Likes jazz", confidence: 0.6})
	setSensitivity(t, ds, survivorPub, sPublic)
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Likes jazz"), 2, &survivorPub, nil, nil, uuid.Nil, nil, nil,
		WithNewMemberSensitivity(sPersonal))
	require.NoError(t, err)
	require.Equal(t, sPublic, sensitivityOf(t, ds, survivorPub))

	// ... but a group with no new member and no sensitive member does not touch the level (a
	// public survivor is not silently raised to the personal default).
	survivor2 := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Has a bird", confidence: 0.6})
	absorbed2 := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Owns a bird", confidence: 0.6})
	setSensitivity(t, ds, survivor2, sPublic)
	setSensitivity(t, ds, absorbed2, sPublic)
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Has a bird"), 2, &survivor2, []uuid.UUID{absorbed2}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, sPublic, sensitivityOf(t, ds, survivor2))
}

func TestPersistMemoryLinkGroup_LeavesLevelsAlone(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	pub := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "technical angle", confidence: 0.6})
	sens := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "emotional angle", confidence: 0.6})
	setSensitivity(t, ds, pub, sPublic)
	setSensitivity(t, ds, sens, sSensitive)

	_, err := ds.PersistMemoryLinkGroup(ctx, userID, chatID, "User", "shared topic", []uuid.UUID{pub, sens},
		[]LinkGroupNewMember{{Content: "narrative angle", Embedding: foldTestVector(1), Sensitivity: sSensitive}, {Content: "plain angle", Embedding: foldTestVector(2)}},
		nil, nil, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, sPublic, sensitivityOf(t, ds, pub), "linking never changes an existing member's level")
	require.Equal(t, sSensitive, sensitivityOf(t, ds, sens))

	rows, err := ds.dbClient.Memory.Query().Where(entmemory.ContentIn("narrative angle", "plain angle")).All(ctx)
	require.NoError(t, err)
	got := map[string]entmemory.Sensitivity{}
	for _, r := range rows {
		got[r.Content] = r.Sensitivity
	}
	require.Equal(t, map[string]entmemory.Sensitivity{"narrative angle": entmemory.SensitivitySensitive, "plain angle": entmemory.SensitivityPersonal}, got)
}

func TestListMemoryMergeEvents_SandboxGate(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	foldedPub := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "public fact", confidence: 0.6})
	foldedPubDup := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "public fact again", confidence: 0.6})
	foldedSens := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "sensitive fact", confidence: 0.6})
	foldedSensDup := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "sensitive fact again", confidence: 0.6})
	linkA := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "link a", confidence: 0.6})
	linkB := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "link b", confidence: 0.6})
	for _, id := range []uuid.UUID{foldedPub, foldedPubDup, linkA, linkB} {
		setSensitivity(t, ds, id, sPublic)
	}
	setSensitivity(t, ds, foldedSens, sSensitive)
	setSensitivity(t, ds, foldedSensDup, sSensitive)

	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("public fact"), 2, &foldedPub, []uuid.UUID{foldedPubDup}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("sensitive fact"), 2, &foldedSens, []uuid.UUID{foldedSensDup}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	_, err = ds.PersistMemoryLinkGroup(ctx, userID, chatID, "User", "link", []uuid.UUID{linkA, linkB}, nil, nil, nil, uuid.Nil)
	require.NoError(t, err)

	list := func(limit *models.MemorySensitivity) map[string]models.MemoryMergeType {
		res, err := ds.ListMemoryMergeEvents(ctx, userID, 1, 50, models.MemoryMergeEventFilters{MaxSensitivity: limit})
		require.NoError(t, err)
		out := map[string]models.MemoryMergeType{}
		for _, r := range res.Results {
			ev := r.(*models.MemoryMergeEvent)
			out[ev.Content] = ev.MergeType
		}
		return out
	}
	all := list(nil)
	require.Len(t, all, 3, "unrestricted sees the folds and the link")

	unrestricted := models.MemorySensitivitySensitive
	require.Len(t, list(&unrestricted), 3, "a sensitive limit is not a restriction")

	personal := sPersonal
	got := list(&personal)
	require.Equal(t, map[string]models.MemoryMergeType{"public fact": models.MemoryMergeTypeFoldLive}, got,
		"a restricted chat sees only fold events whose survivor is within its limit, never link events")
}
