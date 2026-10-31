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

// ADR 0x036: an agent's remove_memory is a soft delete with an audit event the user can undo.

func TestRemoveMemory_SoftDeletesAndRecordsTheRemoval(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.9, embedding: foldTestVector(1)})
	personalityID := uuid.New()

	event, err := ds.RemoveMemory(ctx, userID, id, models.MemoryRemoval{PersonalityID: &personalityID, ChatID: &chatID, Reason: "  moved to Lisbon  "})
	require.NoError(t, err)
	require.Equal(t, models.MemoryMergeTypeRemove, event.MergeType)
	require.Equal(t, id, event.SurvivorMemoryID)
	require.Equal(t, "Lives in Berlin", event.Content)
	require.Nil(t, event.Snapshot, "a remove event has no fold state")
	require.NotNil(t, event.Removal)
	require.Equal(t, "moved to Lisbon", event.Removal.Reason)
	require.Equal(t, &personalityID, event.Removal.PersonalityID)
	require.Equal(t, &chatID, event.Removal.ChatID)
	require.Len(t, event.SourceMembers, 1)
	require.Equal(t, &id, event.SourceMembers[0].MemoryID)

	mem, err := ds.GetMemory(ctx, userID, id)
	require.NoError(t, err)
	require.Equal(t, models.MemoryStatusInactive, mem.Status)
	require.NotNil(t, mem.RemovedAt)
	require.Equal(t, &personalityID, mem.RemovedByPersonalityID)
	require.Equal(t, &chatID, mem.RemovedByChatID)
	require.Equal(t, foldTestVector(1), foldTestState(t, ds, id).Embedding, "the embedding is kept so undo needs no re-embed")

	listed, err := ds.ListMemories(ctx, userID, 1, 20, models.MemoryFilters{})
	require.NoError(t, err)
	require.Zero(t, listed.TotalCount, "a removed memory is out of the default (active) listing")

	events, err := ds.ListMemoryMergeEvents(ctx, userID, 1, 20, models.MemoryMergeEventFilters{SurvivorMemoryID: &id})
	require.NoError(t, err)
	require.Equal(t, 1, events.TotalCount)
	listedEvent := events.Results[0].(*models.MemoryMergeEvent)
	require.Equal(t, "moved to Lisbon", listedEvent.Removal.Reason)
}

func TestRemoveMemory_Refusals(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	starred := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "starred", confidence: 0.6, starred: true})
	summary := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "summary", confidence: 0.6, scope: entmemory.ScopeSummary})
	merged := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "merged away", confidence: 0.6})
	_, err := ds.dbClient.Memory.UpdateOneID(merged).SetStatus(entmemory.StatusInactive).Save(ctx)
	require.NoError(t, err)
	removed := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "removed", confidence: 0.6})
	_, err = ds.RemoveMemory(ctx, userID, removed, models.MemoryRemoval{ChatID: &chatID})
	require.NoError(t, err)
	plain := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "plain", confidence: 0.6})

	cases := []struct {
		name    string
		id      uuid.UUID
		reason  string
		want    error
		wantMsg string
	}{
		{name: "starred", id: starred, want: ErrMemoryNotRemovable, wantMsg: "starred"},
		{name: "summary", id: summary, want: ErrMemoryNotRemovable, wantMsg: "checkpoint summary"},
		{name: "archived or merged", id: merged, want: ErrMemoryNotRemovable, wantMsg: "not in active memory"},
		{name: "already removed", id: removed, want: ErrMemoryNotRemovable, wantMsg: "already removed"},
		{name: "reason too long", id: plain, reason: strings.Repeat("x", models.MaxMemoryRemovalReasonRunes+1), want: ErrMemoryNotRemovable, wantMsg: "reason"},
		{name: "unknown id", id: uuid.New(), want: ErrMemoryNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ds.RemoveMemory(ctx, userID, tc.id, models.MemoryRemoval{ChatID: &chatID, Reason: tc.reason})
			require.ErrorIs(t, err, tc.want)
			require.Contains(t, err.Error(), tc.wantMsg)
		})
	}

	// Another user's memory looks missing.
	_, err = ds.RemoveMemory(ctx, uuid.New(), plain, models.MemoryRemoval{})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Equal(t, entmemory.StatusActive, foldTestState(t, ds, plain).Status)
	require.Equal(t, entmemory.StatusActive, foldTestState(t, ds, starred).Status)
}

func TestRemoveMemory_UndoRestoresTheMemory(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.6, embedding: foldTestVector(2)})

	event, err := ds.RemoveMemory(ctx, userID, id, models.MemoryRemoval{ChatID: &chatID})
	require.NoError(t, err)

	undone, err := ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)
	require.NotNil(t, undone.RevertedAt)

	mem, err := ds.GetMemory(ctx, userID, id)
	require.NoError(t, err)
	require.Equal(t, models.MemoryStatusActive, mem.Status)
	require.Nil(t, mem.RemovedAt)
	require.Nil(t, mem.RemovedByChatID)
	require.Equal(t, foldTestVector(2), foldTestState(t, ds, id).Embedding)

	// Restored, it can be removed again.
	_, err = ds.RemoveMemory(ctx, userID, id, models.MemoryRemoval{ChatID: &chatID})
	require.NoError(t, err)
}

// The user setting a removed memory active again restores it and closes its remove event, so that
// event cannot later be undone on top of a newer removal.
func TestRemoveMemory_UserReactivationClosesTheEvent(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.6})

	first, err := ds.RemoveMemory(ctx, userID, id, models.MemoryRemoval{ChatID: &chatID})
	require.NoError(t, err)

	active := models.MemoryStatusActive
	mem, err := ds.UpdateMemory(ctx, userID, id, models.MemoryPatch{Status: &active})
	require.NoError(t, err)
	require.Equal(t, models.MemoryStatusActive, mem.Status)
	require.Nil(t, mem.RemovedAt)

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, first.ID)
	require.ErrorIs(t, err, ErrMemoryMergeAlreadyReverted)

	second, err := ds.RemoveMemory(ctx, userID, id, models.MemoryRemoval{ChatID: &chatID})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	mem, err = ds.GetMemory(ctx, userID, id)
	require.NoError(t, err)
	require.NotNil(t, mem.RemovedAt, "the newer removal stands")

	// Any other patch leaves the removal alone.
	starred := true
	mem, err = ds.UpdateMemory(ctx, userID, id, models.MemoryPatch{Starred: &starred})
	require.NoError(t, err)
	require.NotNil(t, mem.RemovedAt)
	require.Equal(t, models.MemoryStatusInactive, mem.Status)
}

// A memory the user deleted after the removal cannot be restored.
func TestRemoveMemory_UndoAfterUserDeleteIsNotFound(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()
	id := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Lives in Berlin", confidence: 0.6})

	event, err := ds.RemoveMemory(ctx, userID, id, models.MemoryRemoval{ChatID: &chatID})
	require.NoError(t, err)
	require.NoError(t, ds.DeleteMemory(ctx, userID, id))

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.True(t, errors.Is(err, ErrMemoryNotFound), "got %v", err)
}
