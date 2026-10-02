package datastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/embedding"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// embeddingRowsFor returns every Embedding row pointing at memoryID.
func embeddingRowsFor(t *testing.T, ds *Datastore, memoryID uuid.UUID) [][]float32 {
	t.Helper()
	rows, err := ds.dbClient.Embedding.Query().
		Where(embedding.HasMemoryWith(entmemory.ID(memoryID))).
		All(context.Background())
	require.NoError(t, err)
	out := make([][]float32, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Embedding.Slice())
	}
	return out
}

func seedEmbedding(t *testing.T, ds *Datastore, memoryID uuid.UUID, vec []float32) {
	t.Helper()
	_, err := ds.dbClient.Embedding.Create().
		SetEmbedding(pgvector.NewVector(vec)).
		SetMemoryID(memoryID).
		Save(context.Background())
	require.NoError(t, err)
}

func candidateIDs(cs []models.MemoryEmbeddingCandidate) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.MemoryID)
	}
	return ids
}

func TestUpdateMemory_ContentChangeDropsStaleEmbedding(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	mem, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "likes green tea", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	seedEmbedding(t, ds, mem.ID, []float32{1, 2})

	// Unchanged content (even with surrounding whitespace) and non-content
	// fields keep the embedding.
	same := "  likes green tea "
	starred := true
	_, err = ds.UpdateMemory(ctx, userID, mem.ID, models.MemoryPatch{Content: &same, Starred: &starred})
	require.NoError(t, err)
	require.Len(t, embeddingRowsFor(t, ds, mem.ID), 1)

	// A real content change removes it in the same transaction.
	changed := "prefers black coffee"
	updated, err := ds.UpdateMemory(ctx, userID, mem.ID, models.MemoryPatch{Content: &changed})
	require.NoError(t, err)
	require.Equal(t, changed, updated.Content)
	require.Empty(t, embeddingRowsFor(t, ds, mem.ID), "edited memory must not keep matching on its old text")

	missing, err := ds.OwnedMemoriesMissingEmbedding(ctx, userID, []uuid.UUID{mem.ID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{mem.ID}, candidateIDs(missing))
	require.Equal(t, changed, missing[0].Content)
}

func TestUpdateMemory_SummaryContentChangeKeepsEmbedding(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, chatID, "summary v1", []float32{1, 2}))
	summary, err := ds.GetChatSummaryMemory(ctx, userID, chatID)
	require.NoError(t, err)

	edited := "summary edited by hand"
	_, err = ds.UpdateMemory(ctx, userID, summary.ID, models.MemoryPatch{Content: &edited})
	require.NoError(t, err)
	require.Len(t, embeddingRowsFor(t, ds, summary.ID), 1, "UpsertChatSummaryMemory owns the Summary embedding")

	// The checkpoint upsert still finds exactly one row to update.
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, chatID, "summary v2", []float32{3, 4}))
	require.Equal(t, [][]float32{{3, 4}}, embeddingRowsFor(t, ds, summary.ID))
}

func TestOwnedMemoriesMissingEmbedding_FiltersOwnerSummaryAndEmbedded(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	otherID := uuid.New()
	createTestUser(t, ds, userID)
	createTestUser(t, ds, otherID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	bare, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "bare", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	embedded, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "embedded", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	seedEmbedding(t, ds, embedded.ID, []float32{1})
	summary, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "summary", Level: models.MemoryLevelSummary, ChatID: &chatID})
	require.NoError(t, err)
	inactive, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "archived", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	inactiveStatus := models.MemoryStatusInactive
	_, err = ds.UpdateMemory(ctx, userID, inactive.ID, models.MemoryPatch{Status: &inactiveStatus})
	require.NoError(t, err)
	theirs, err := ds.CreateMemoryFromInput(ctx, otherID, models.CreateMemoryInput{Content: "theirs", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	got, err := ds.OwnedMemoriesMissingEmbedding(ctx, userID, []uuid.UUID{bare.ID, embedded.ID, summary.ID, inactive.ID, theirs.ID})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{bare.ID, inactive.ID}, candidateIDs(got))

	empty, err := ds.OwnedMemoriesMissingEmbedding(ctx, userID, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestListMemoriesMissingEmbedding_ActiveNonSummaryWithCursor(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	otherID := uuid.New()
	createTestUser(t, ds, userID)
	createTestUser(t, ds, otherID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	// SQLite compares timestamps as text, so pin created_at to whole UTC
	// seconds (as TestMemoryCursorPredicate does) to make the keyset cursor
	// comparison meaningful here; Postgres compares timestamptz natively.
	base := time.Now().UTC().Truncate(time.Second)
	seed := func(i int, owner uuid.UUID, scope entmemory.Scope, status entmemory.Status) uuid.UUID {
		create := ds.dbClient.Memory.Create().
			SetContent("memory").
			SetScope(scope).
			SetStatus(status).
			SetOwnerID(owner).
			SetCreatedAt(base.Add(time.Duration(i) * time.Second))
		if scope == entmemory.ScopeSummary {
			create.SetChatID(chatID)
		}
		m, err := create.Save(ctx)
		require.NoError(t, err)
		return m.ID
	}
	first := seed(0, userID, entmemory.ScopeUser, entmemory.StatusActive)
	second := seed(1, otherID, entmemory.ScopeUser, entmemory.StatusActive)
	embedded := seed(2, userID, entmemory.ScopeUser, entmemory.StatusActive)
	seedEmbedding(t, ds, embedded, []float32{1})
	seed(3, userID, entmemory.ScopeSummary, entmemory.StatusActive)
	seed(4, userID, entmemory.ScopeUser, entmemory.StatusInactive)

	all, err := ds.ListMemoriesMissingEmbedding(ctx, time.Time{}, uuid.Nil, 10)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, candidateIDs(all), "active, non-Summary, unembedded rows across users, oldest first")

	page1, err := ds.ListMemoriesMissingEmbedding(ctx, time.Time{}, uuid.Nil, 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first}, candidateIDs(page1))
	page2, err := ds.ListMemoriesMissingEmbedding(ctx, page1[0].CreatedAt, page1[0].MemoryID, 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{second}, candidateIDs(page2))
	page3, err := ds.ListMemoriesMissingEmbedding(ctx, page2[0].CreatedAt, page2[0].MemoryID, 1)
	require.NoError(t, err)
	require.Empty(t, page3)
}

func TestSetMemoryEmbedding_ReplacesAndGuardsContent(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	// A memory created by the create_memory tool already has a randomly keyed row.
	agentMem, err := ds.CreateMemory(ctx, userID, models.Memory{Content: "tool memory", Scope: string(entmemory.ScopeUser)}, []float32{9, 9}, uuid.Nil)
	require.NoError(t, err)

	wrote, err := ds.SetMemoryEmbedding(ctx, agentMem.ID, "tool memory", []float32{1, 2})
	require.NoError(t, err)
	require.True(t, wrote)
	require.Equal(t, [][]float32{{1, 2}}, embeddingRowsFor(t, ds, agentMem.ID), "old row replaced, not duplicated")

	// Writing again upserts the same row.
	wrote, err = ds.SetMemoryEmbedding(ctx, agentMem.ID, "tool memory", []float32{3, 4})
	require.NoError(t, err)
	require.True(t, wrote)
	require.Equal(t, [][]float32{{3, 4}}, embeddingRowsFor(t, ds, agentMem.ID))

	// A vector computed from stale content is not written.
	wrote, err = ds.SetMemoryEmbedding(ctx, agentMem.ID, "what it said before an edit", []float32{5, 6})
	require.NoError(t, err)
	require.False(t, wrote)
	require.Equal(t, [][]float32{{3, 4}}, embeddingRowsFor(t, ds, agentMem.ID))

	// Summary memories and missing memories are left alone.
	summary, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "summary", Level: models.MemoryLevelSummary, ChatID: &chatID})
	require.NoError(t, err)
	wrote, err = ds.SetMemoryEmbedding(ctx, summary.ID, "summary", []float32{1})
	require.NoError(t, err)
	require.False(t, wrote)
	require.Empty(t, embeddingRowsFor(t, ds, summary.ID))

	wrote, err = ds.SetMemoryEmbedding(ctx, uuid.New(), "nothing", []float32{1})
	require.NoError(t, err)
	require.False(t, wrote)

	_, err = ds.SetMemoryEmbedding(ctx, agentMem.ID, "tool memory", nil)
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestBackfillMemoryEmbeddings_EmbedsMissingAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	var ids []uuid.UUID
	for _, content := range []string{"one", "two", "three"} {
		m, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: content, Level: models.MemoryLevelGlobal})
		require.NoError(t, err)
		ids = append(ids, m.ID)
		time.Sleep(2 * time.Millisecond)
	}

	var calls [][]string
	embed := func(_ context.Context, inputs []string) ([][]float32, error) {
		calls = append(calls, append([]string(nil), inputs...))
		out := make([][]float32, len(inputs))
		for i := range inputs {
			out[i] = []float32{float32(len(inputs[i]))}
		}
		return out, nil
	}

	stats, err := ds.BackfillMemoryEmbeddings(ctx, 2, embed)
	require.NoError(t, err)
	require.Equal(t, MemoryEmbeddingBackfillStats{Embedded: 3}, stats)
	require.Equal(t, [][]string{{"one", "two"}, {"three"}}, calls)
	for _, id := range ids {
		require.Len(t, embeddingRowsFor(t, ds, id), 1)
	}

	calls = nil
	stats, err = ds.BackfillMemoryEmbeddings(ctx, 2, embed)
	require.NoError(t, err)
	require.Equal(t, MemoryEmbeddingBackfillStats{}, stats)
	require.Empty(t, calls, "nothing left to embed on a second run")
}

func TestBackfillMemoryEmbeddings_RetriesBatchItemsIndividually(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	good, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "good", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	bad, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "poison", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	embed := func(_ context.Context, inputs []string) ([][]float32, error) {
		for _, in := range inputs {
			if in == "poison" {
				return nil, errors.New("input rejected")
			}
		}
		out := make([][]float32, len(inputs))
		for i := range inputs {
			out[i] = []float32{1}
		}
		return out, nil
	}

	stats, err := ds.BackfillMemoryEmbeddings(ctx, 10, embed)
	require.NoError(t, err)
	require.Equal(t, MemoryEmbeddingBackfillStats{Embedded: 1, Failed: 1}, stats)
	require.Len(t, embeddingRowsFor(t, ds, good.ID), 1)
	require.Empty(t, embeddingRowsFor(t, ds, bad.ID))
}

func TestBackfillMemoryEmbeddings_StopsWhenProviderUnavailable(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	for _, content := range []string{"one", "two", "three"} {
		_, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: content, Level: models.MemoryLevelGlobal})
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}

	calls := 0
	embed := func(_ context.Context, inputs []string) ([][]float32, error) {
		calls++
		return nil, errors.New("network denied")
	}

	stats, err := ds.BackfillMemoryEmbeddings(ctx, 2, embed)
	require.Error(t, err)
	require.Equal(t, MemoryEmbeddingBackfillStats{Failed: 2}, stats)
	require.Equal(t, 3, calls, "one batch call plus one retry per item, then stop")
}
