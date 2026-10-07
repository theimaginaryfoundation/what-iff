package datastore

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Memory provenance: a Discord relay thread's memories are external (learned from people
// outside the account) and attributed to a speaker; merges keep external if any member is and a
// speaker only when every member agrees; the manager can filter and relabel; export/import carry it.

var (
	pUser     = models.MemoryProvenanceUser
	pExternal = models.MemoryProvenanceExternal
)

func originOf(t *testing.T, ds *Datastore, id uuid.UUID) models.MemoryOrigin {
	t.Helper()
	row, err := ds.dbClient.Memory.Get(context.Background(), id)
	require.NoError(t, err)
	return models.OriginFrom(models.MemoryProvenance(row.Provenance), row.SourceSpeaker)
}

func setOrigin(t *testing.T, ds *Datastore, id uuid.UUID, o models.MemoryOrigin) {
	t.Helper()
	upd := ds.dbClient.Memory.UpdateOneID(id).SetProvenance(entmemory.Provenance(o.Provenance))
	if s := o.SpeakerPtr(); s != nil {
		upd = upd.SetSourceSpeaker(*s)
	} else {
		upd = upd.ClearSourceSpeaker()
	}
	require.NoError(t, upd.Exec(context.Background()))
}

// createGlobalMemory stores a User-scoped memory typed in the memory manager.
func createGlobalMemory(t *testing.T, ds *Datastore, userID uuid.UUID, content string) *models.Memory {
	t.Helper()
	mem, err := ds.CreateMemoryFromInput(context.Background(), userID, models.CreateMemoryInput{
		Content: content, Level: models.MemoryLevelGlobal,
	})
	require.NoError(t, err)
	return mem
}

func ext(speaker string) models.MemoryOrigin {
	return models.MemoryOrigin{Provenance: pExternal, Speaker: speaker}
}

func TestIsExternalRelayChat_IsAnyChatTheUsersBotIsBoundTo(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()

	ok, err := f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.False(t, ok, "an ordinary chat")

	require.NoError(t, f.ds.dbClient.Chat.UpdateOneID(f.chat).SetContextScope(entchat.ContextScopeSandbox).Exec(ctx))
	ok, err = f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.False(t, ok, "sandboxed but not bound")

	f.binding(t, f.bot(t).ID, "c1")
	ok, err = f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.True(t, ok, "a relay thread")

	ok, err = f.ds.IsExternalRelayChat(ctx, f.other, f.chat)
	require.NoError(t, err)
	require.False(t, ok, "another account's chat is never one")

	require.NoError(t, f.ds.dbClient.Chat.UpdateOneID(f.chat).SetContextScope(entchat.ContextScopeAccount).Exec(ctx))
	ok, err = f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.True(t, ok, "unsandboxing a relay thread does not change who talks in it")
}

func TestExternalSpeakerForMessage_ReadsTheInboundAuthorOwnerScoped(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	b := f.binding(t, f.bot(t).ID, "c1")
	link, _, err := f.ds.RecordInboundDiscordMessage(ctx, models.DiscordInbound{BindingID: b.ID, DiscordMessageID: "m1", DiscordChannelID: "c1", AuthorID: "9", AuthorName: "alice"})
	require.NoError(t, err)
	msgID := uuid.New()
	require.NoError(t, f.ds.AttachDiscordLinkMessage(ctx, link.ID, msgID))

	name, err := f.ds.ExternalSpeakerForMessage(ctx, f.owner, msgID)
	require.NoError(t, err)
	require.Equal(t, "alice", name)
	name, err = f.ds.ExternalSpeakerForMessage(ctx, f.other, msgID)
	require.NoError(t, err)
	require.Equal(t, "", name, "another account learns nothing")
	name, err = f.ds.ExternalSpeakerForMessage(ctx, f.owner, uuid.New())
	require.NoError(t, err)
	require.Equal(t, "", name, "a message typed in the app has no Discord author")
}

func TestUpsertChatSummaryMemory_RelayThreadSummaryIsExternal(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	require.NoError(t, f.ds.dbClient.Chat.UpdateOneID(f.chat).SetContextScope(entchat.ContextScopeSandbox).Exec(ctx))

	require.NoError(t, f.ds.UpsertChatSummaryMemory(ctx, f.owner, f.chat, "talked about tea", []float32{1}))
	sum, err := f.ds.GetChatSummaryMemory(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.Equal(t, pUser, sum.Provenance, "not bound yet")

	f.binding(t, f.bot(t).ID, "c1")
	require.NoError(t, f.ds.UpsertChatSummaryMemory(ctx, f.owner, f.chat, "talked about tea with alice", []float32{1}))
	sum, err = f.ds.GetChatSummaryMemory(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.Equal(t, pExternal, sum.Provenance)

	// Unbinding or unsandboxing the thread later does not launder what was already said there.
	require.NoError(t, f.ds.DeleteDiscordBot(ctx, f.owner, mustOnlyBot(t, f)))
	require.NoError(t, f.ds.dbClient.Chat.UpdateOneID(f.chat).SetContextScope(entchat.ContextScopeAccount).Exec(ctx))
	require.NoError(t, f.ds.UpsertChatSummaryMemory(ctx, f.owner, f.chat, "and more", []float32{1}))
	sum, err = f.ds.GetChatSummaryMemory(ctx, f.owner, f.chat)
	require.NoError(t, err)
	require.Equal(t, pExternal, sum.Provenance)
}

func TestCreateMemory_StoresProvenanceAndSpeakerOnlyForExternal(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)

	alice := "alice"
	created, err := ds.CreateMemory(ctx, userID, models.Memory{Content: "likes tea", Scope: "User", Provenance: pExternal, SourceSpeaker: &alice}, nil, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, ext("alice"), originOf(t, ds, created.ID))

	created, err = ds.CreateMemory(ctx, userID, models.Memory{Content: "own fact", Scope: "User", SourceSpeaker: &alice}, nil, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pUser}, originOf(t, ds, created.ID), "a user memory never carries a speaker")

	manual := createGlobalMemory(t, ds, userID, "typed in the manager")
	require.Equal(t, pUser, manual.Provenance)
}

func TestMemoryProvenance_ListFilterPatchAndExportImport(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	dsDst, cleanupDst := newMemoryTestDatastore(t)
	defer cleanupDst()
	userID, dst := uuid.New(), uuid.New()
	createTestUser(t, ds, userID)
	createTestUser(t, dsDst, dst)

	own := createGlobalMemory(t, ds, userID, "own")
	external := createGlobalMemory(t, ds, userID, "heard on discord")
	setOrigin(t, ds, external.ID, ext("alice"))

	filter := pExternal
	page, err := ds.ListMemories(ctx, userID, 1, 50, models.MemoryFilters{Provenance: &filter})
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	got := page.Results[0].(*models.Memory)
	require.Equal(t, external.ID, got.ID)
	require.Equal(t, pExternal, got.Provenance)
	require.NotNil(t, got.SourceSpeaker)
	require.Equal(t, "alice", *got.SourceSpeaker)

	var buf bytes.Buffer
	require.NoError(t, ds.ExportMemories(ctx, userID, &buf))
	records, err := ds.ExportMemoryRecords(ctx, userID)
	require.NoError(t, err)
	for _, r := range records {
		switch r.ID {
		case own.ID:
			require.Equal(t, models.MemoryProvenance(""), r.Provenance, "the user default is not written")
		case external.ID:
			require.Equal(t, pExternal, r.Provenance)
			require.Equal(t, "alice", *r.SourceSpeaker)
		}
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	_, err = dsDst.ImportMemories(ctx, dst, zr, func(context.Context, string) ([]float32, error) { return []float32{1}, nil })
	require.NoError(t, err)
	rows, err := dsDst.dbClient.Memory.Query().Where(entmemory.HasOwnerWith(user.ID(dst))).All(ctx)
	require.NoError(t, err)
	for _, r := range rows {
		o := models.OriginFrom(models.MemoryProvenance(r.Provenance), r.SourceSpeaker)
		if r.Content == "heard on discord" {
			require.Equal(t, ext("alice"), o)
		} else {
			require.Equal(t, models.MemoryOrigin{Provenance: pUser}, o)
		}
	}

	// The owner can confirm an external memory as their own.
	verified := pUser
	updated, err := ds.UpdateMemory(ctx, userID, external.ID, models.MemoryPatch{Provenance: &verified})
	require.NoError(t, err)
	require.Equal(t, pUser, updated.Provenance)
	require.Nil(t, updated.SourceSpeaker, "a user memory reports no speaker")
	require.Equal(t, models.MemoryOrigin{Provenance: pUser}, originOf(t, ds, external.ID), "and stores none")
	bad := models.MemoryProvenance("discord")
	_, err = ds.UpdateMemory(ctx, userID, own.ID, models.MemoryPatch{Provenance: &bad})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestPersistMemoryMergeGroup_ExternalIfAnyMemberAndSpeakerOnlyWhenAllAgree(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	// A user survivor absorbing an external memory becomes external; the user member names no
	// speaker, so none survives. Undo puts the survivor back.
	own := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Likes tea", confidence: 0.6})
	heard := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "likes tea", confidence: 0.6})
	setOrigin(t, ds, heard, ext("alice"))
	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Likes tea"), 2, &own, []uuid.UUID{heard}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pExternal}, originOf(t, ds, own))
	event := foldTestEvent(t, ds, userID, own)
	require.True(t, event.Snapshot.OriginChanged)
	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pUser}, originOf(t, ds, own))

	// An external survivor keeps its speaker when the new member names the same one.
	a := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Has a cat", confidence: 0.6})
	setOrigin(t, ds, a, ext("alice"))
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Has a cat"), 2, &a, nil, nil, uuid.Nil, nil, nil,
		WithNewMemberOrigin(ext("Alice")))
	require.NoError(t, err)
	require.Equal(t, ext("alice"), originOf(t, ds, a))

	// ... and loses it when another speaker says the same thing; undo restores it.
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Has a cat"), 2, &a, nil, nil, uuid.Nil, nil, nil,
		WithNewMemberOrigin(ext("bob")))
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pExternal}, originOf(t, ds, a))
	events, err := ds.dbClient.MemoryMergeEvent.Query().All(ctx)
	require.NoError(t, err)
	last := events[len(events)-1]
	_, err = ds.UndoMemoryMergeEvent(ctx, userID, last.ID)
	require.NoError(t, err)
	require.Equal(t, ext("alice"), originOf(t, ds, a))

	// A user fold of user memories leaves the origin alone (no snapshot noise).
	b := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Runs", confidence: 0.6})
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Runs"), 2, &b, nil, nil, uuid.Nil, nil, nil,
		WithNewMemberOrigin(models.MemoryOrigin{Provenance: pUser}))
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pUser}, originOf(t, ds, b))
	require.False(t, foldTestEvent(t, ds, userID, b).Snapshot.OriginChanged)

	// A new-only group is created with the new members' origin.
	mem, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Plays chess"), 1, nil, nil, foldTestVector(1), uuid.Nil, nil, nil,
		WithNewMemberOrigin(ext("carol")))
	require.NoError(t, err)
	require.Equal(t, ext("carol"), originOf(t, ds, mem.ID))
	require.Equal(t, pExternal, mem.Provenance)
}

func TestUndoFold_KeepsAProvenanceTheUserSetSinceTheFold(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	own := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Likes jazz", confidence: 0.6})
	setOrigin(t, ds, own, ext("alice"))
	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Likes jazz"), 2, &own, nil, nil, uuid.Nil, nil, nil,
		WithNewMemberOrigin(ext("bob")))
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pExternal}, originOf(t, ds, own))

	// The owner verifies it, then undoes the fold: their label wins over the snapshot's.
	verified := pUser
	_, err = ds.UpdateMemory(ctx, userID, own, models.MemoryPatch{Provenance: &verified})
	require.NoError(t, err)
	_, err = ds.UndoMemoryMergeEvent(ctx, userID, foldTestEvent(t, ds, userID, own).ID)
	require.NoError(t, err)
	require.Equal(t, models.MemoryOrigin{Provenance: pUser}, originOf(t, ds, own))
}

func TestPersistMemoryLinkGroup_NewMembersCarryTheirOrigin(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	existing := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Went hiking", confidence: 0.6})
	_, err := ds.PersistMemoryLinkGroup(ctx, userID, chatID, "User", "hiking", []uuid.UUID{existing},
		[]LinkGroupNewMember{{Content: "Hiking felt freeing", Embedding: foldTestVector(1), Origin: ext("alice")}}, nil, nil, uuid.Nil)
	require.NoError(t, err)
	rows, err := ds.dbClient.Memory.Query().Where(entmemory.ContentEQ("Hiking felt freeing")).All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, ext("alice"), originOf(t, ds, rows[0].ID))
	require.Equal(t, models.MemoryOrigin{Provenance: pUser}, originOf(t, ds, existing), "linking never relabels an existing member")
}

func mustOnlyBot(t *testing.T, f *discordFixture) uuid.UUID {
	t.Helper()
	bots, err := f.ds.ListDiscordBots(context.Background(), f.owner)
	require.NoError(t, err)
	require.Len(t, bots, 1)
	return bots[0].ID
}
