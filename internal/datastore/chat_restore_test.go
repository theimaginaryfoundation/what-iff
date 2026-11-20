package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestRestoreChat_AtomicOwnershipAndPreparation(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	owner, id := uuid.New(), uuid.New()
	createTestUser(t, ds, owner)
	createTestChat(t, ds, id, owner)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(id).SetArchived(true).SetCheckpointSummary("old summary").SetResponseID("old-chain").Exec(ctx))

	_, err := ds.RestoreChat(ctx, uuid.New(), id, "Keep decisions")
	require.ErrorIs(t, err, ErrChatRestoreConflict)
	stored, err := ds.dbClient.Chat.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, stored.Archived)
	require.Empty(t, stored.RestoreGuidance)

	require.NoError(t, ds.dbClient.Chat.UpdateOneID(id).SetRehydrationState(models.RehydrationStateProcessing).Exec(ctx))
	_, err = ds.RestoreChat(ctx, owner, id, "Keep decisions")
	require.ErrorIs(t, err, ErrChatRestoreConflict)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(id).SetRehydrationState(models.RehydrationStateReady).SetCheckpointStartedAt(time.Now()).Exec(ctx))
	_, err = ds.RestoreChat(ctx, owner, id, "Keep decisions")
	require.ErrorIs(t, err, ErrChatRestoreConflict)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(id).ClearCheckpointStartedAt().Exec(ctx))

	restored, err := ds.RestoreChat(ctx, owner, id, " Keep decisions. Omit old drafts. ")
	require.NoError(t, err)
	require.False(t, *restored.Archived)
	require.Equal(t, "Keep decisions. Omit old drafts.", restored.RestoreGuidance)
	require.Equal(t, models.RehydrationStateStale, restored.RehydrationState)
	require.Nil(t, restored.ResponseID, "provider response chains must not replay old context")
	require.Equal(t, "old summary", restored.CheckpointSummary, "stored history is not erased")
	info, err := ds.GetChatRehydrationInfo(ctx, owner, id)
	require.NoError(t, err)
	require.True(t, info.NeedsRehydration(), "native threads with guidance are eligible even with an old summary")
	claimed, err := ds.ClaimChatRehydration(ctx, owner, id, false)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = ds.ClaimChatRehydration(ctx, owner, id, false)
	require.NoError(t, err)
	require.False(t, claimed, "only one worker may prepare the guided summary")
}

func TestRestoreChat_BlankKeepsNormalContext(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	owner, id := uuid.New(), uuid.New()
	createTestUser(t, ds, owner)
	createTestChat(t, ds, id, owner)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(id).SetArchived(true).SetResponseID("chain").Exec(ctx))
	restored, err := ds.RestoreChat(ctx, owner, id, " \n ")
	require.NoError(t, err)
	require.Empty(t, restored.RestoreGuidance)
	require.Empty(t, restored.RehydrationState)
	require.Equal(t, "chain", *restored.ResponseID)
}
