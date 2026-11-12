package datastore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ADR 0x036: an agent's archive_memory archives like a merge does (inactive, nothing deleted) and
// logs an event the user can undo.

func TestArchiveMemory_ArchivesAndRecordsWhoAndWhy(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.9, embedding: foldTestVector(1)})
	personalityID := uuid.New()

	event, err := ds.ArchiveMemory(ctx, userID, id, models.MemoryArchival{PersonalityID: &personalityID, ChatID: &chatID, Reason: "  moved to Lisbon  "})
	require.NoError(t, err)
	require.Equal(t, models.MemoryMergeTypeArchive, event.MergeType)
	require.Equal(t, id, event.SurvivorMemoryID)
	require.Equal(t, "Lives in Berlin", event.Content)
	require.Nil(t, event.Snapshot, "an archive event has no fold state")
	require.NotNil(t, event.Archival)
	require.Equal(t, "moved to Lisbon", event.Archival.Reason)
	require.Equal(t, &personalityID, event.Archival.PersonalityID)
	require.Equal(t, &chatID, event.Archival.ChatID)
	require.Len(t, event.SourceMembers, 1)
	require.Equal(t, &id, event.SourceMembers[0].MemoryID)

	mem, err := ds.GetMemory(ctx, userID, id)
	require.NoError(t, err)
	require.Equal(t, models.MemoryStatusInactive, mem.Status)
	require.Equal(t, "Lives in Berlin", mem.Content, "the row and its content are kept")
	require.NotNil(t, mem.ArchivedAt)
	require.Equal(t, &personalityID, mem.ArchivedByPersonalityID)
	require.Equal(t, &chatID, mem.ArchivedByChatID)
	require.Equal(t, foldTestVector(1), foldTestState(t, ds, id).Embedding, "the embedding is kept so a restore needs no re-embed")

	active, err := ds.ListMemories(ctx, userID, 1, 20, models.MemoryFilters{})
	require.NoError(t, err)
	require.Zero(t, active.TotalCount, "an archived memory is out of the default (active) listing")
	inactive := models.MemoryStatusInactive
	archived, err := ds.ListMemories(ctx, userID, 1, 20, models.MemoryFilters{Status: &inactive})
	require.NoError(t, err)
	require.Equal(t, 1, archived.TotalCount, "it is in the Archived view, where the user can restore it")

	events, err := ds.ListMemoryMergeEvents(ctx, userID, 1, 20, models.MemoryMergeEventFilters{SurvivorMemoryID: &id})
	require.NoError(t, err)
	require.Equal(t, 1, events.TotalCount)
	require.Equal(t, "moved to Lisbon", events.Results[0].(*models.MemoryMergeEvent).Archival.Reason)
}

func TestArchiveMemory_Refusals(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	starred := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "starred", confidence: 0.6, starred: true})
	summary := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "summary", confidence: 0.6, scope: entmemory.ScopeSummary})
	inactive := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "archived by the user or merged away", confidence: 0.6})
	_, err := ds.dbClient.Memory.UpdateOneID(inactive).SetStatus(entmemory.StatusInactive).Save(ctx)
	require.NoError(t, err)
	archived := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "archived by an agent", confidence: 0.6})
	_, err = ds.ArchiveMemory(ctx, userID, archived, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)
	plain := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "plain", confidence: 0.6})

	cases := []struct {
		name    string
		id      uuid.UUID
		reason  string
		want    error
		wantMsg string
	}{
		{name: "starred", id: starred, want: ErrMemoryNotArchivable, wantMsg: "starred"},
		{name: "summary", id: summary, want: ErrMemoryNotArchivable, wantMsg: "checkpoint summary"},
		{name: "already inactive", id: inactive, want: ErrMemoryNotArchivable, wantMsg: "already out of active memory"},
		{name: "already archived by an agent", id: archived, want: ErrMemoryNotArchivable, wantMsg: "already archived"},
		{name: "reason too long", id: plain, reason: strings.Repeat("x", models.MaxMemoryArchiveReasonRunes+1), want: ErrMemoryNotArchivable, wantMsg: "reason"},
		{name: "unknown id", id: uuid.New(), want: ErrMemoryNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ds.ArchiveMemory(ctx, userID, tc.id, models.MemoryArchival{ChatID: &chatID, Reason: tc.reason})
			require.ErrorIs(t, err, tc.want)
			require.Contains(t, err.Error(), tc.wantMsg)
		})
	}

	// Another user's memory looks missing.
	_, err = ds.ArchiveMemory(ctx, uuid.New(), plain, models.MemoryArchival{})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Equal(t, entmemory.StatusActive, foldTestState(t, ds, plain).Status)
	require.Equal(t, entmemory.StatusActive, foldTestState(t, ds, starred).Status)
}

func TestArchiveMemory_UndoRestoresTheMemory(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.6, embedding: foldTestVector(2)})

	event, err := ds.ArchiveMemory(ctx, userID, id, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)

	undone, err := ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)
	require.NotNil(t, undone.RevertedAt)

	mem, err := ds.GetMemory(ctx, userID, id)
	require.NoError(t, err)
	require.Equal(t, models.MemoryStatusActive, mem.Status)
	require.Nil(t, mem.ArchivedAt)
	require.Nil(t, mem.ArchivedByChatID)
	require.Equal(t, foldTestVector(2), foldTestState(t, ds, id).Embedding)

	// Restored, it can be archived again.
	_, err = ds.ArchiveMemory(ctx, userID, id, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)
}

// The user unarchiving a memory an agent archived restores it and closes its archive event, so
// that event cannot later be undone on top of a newer archive.
func TestArchiveMemory_UserUnarchiveClosesTheEvent(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.6})

	first, err := ds.ArchiveMemory(ctx, userID, id, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)

	active := models.MemoryStatusActive
	mem, err := ds.UpdateMemory(ctx, userID, id, models.MemoryPatch{Status: &active})
	require.NoError(t, err)
	require.Equal(t, models.MemoryStatusActive, mem.Status)
	require.Nil(t, mem.ArchivedAt)

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, first.ID)
	require.ErrorIs(t, err, ErrMemoryMergeAlreadyReverted)

	second, err := ds.ArchiveMemory(ctx, userID, id, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	mem, err = ds.GetMemory(ctx, userID, id)
	require.NoError(t, err)
	require.NotNil(t, mem.ArchivedAt, "the newer archive stands")

	// Any other patch leaves the archive alone.
	starred := true
	mem, err = ds.UpdateMemory(ctx, userID, id, models.MemoryPatch{Starred: &starred})
	require.NoError(t, err)
	require.NotNil(t, mem.ArchivedAt)
	require.Equal(t, models.MemoryStatusInactive, mem.Status)
}

// A memory the user deleted after the archive cannot be restored.
func TestArchiveMemory_UndoAfterUserDeleteIsNotFound(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.6})

	event, err := ds.ArchiveMemory(ctx, userID, id, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)
	require.NoError(t, ds.DeleteMemory(ctx, userID, id))

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.True(t, errors.Is(err, ErrMemoryNotFound), "got %v", err)
}

// The replay re-checks (ActiveMemoryIDs for an ordinary chat, MemoryIDsCreatedInChat for a
// sandbox) leave out an archived memory, so it is not shown again from an earlier turn.
func TestArchiveMemory_ReplayLookupsSkipArchivedMemories(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	kept := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "kept", confidence: 0.6, scope: entmemory.ScopeChat})
	gone := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "archived", confidence: 0.6, scope: entmemory.ScopeChat})
	for _, id := range []uuid.UUID{kept, gone} {
		_, err := ds.dbClient.Memory.UpdateOneID(id).SetChatID(chatID).Save(ctx)
		require.NoError(t, err)
	}
	_, err := ds.ArchiveMemory(ctx, userID, gone, models.MemoryArchival{ChatID: &chatID})
	require.NoError(t, err)

	active, err := ds.ActiveMemoryIDs(ctx, userID, []uuid.UUID{kept, gone, uuid.New()})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]struct{}{kept: {}}, active)

	other, err := ds.ActiveMemoryIDs(ctx, uuid.New(), []uuid.UUID{kept})
	require.NoError(t, err)
	require.Empty(t, other, "another user's memory is not theirs to replay")

	inChat, err := ds.MemoryIDsCreatedInChat(ctx, userID, chatID, []uuid.UUID{kept, gone})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]struct{}{kept: {}}, inChat)
}
