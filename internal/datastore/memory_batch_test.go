package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// This file extends memory2_test.go's batch delete/patch coverage. It reuses
// that file's helpers (newMemoryTestDatastore, createTestUser, createTestChat,
// createTestPersonality) since they live in the same package.

// --- DeleteMemoriesBatch: gaps not covered by memory2_test.go ---

func TestDeleteMemoriesBatch_EmptyIDsNoOp(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	result, err := ds.DeleteMemoriesBatch(ctx, uuid.New(), models.BatchDeleteMemoryInput{})
	require.NoError(t, err)
	require.Equal(t, 0, result.DeletedCount)
}

func TestDeleteMemoriesBatch_PartialSkipsWrongOwnerMemory(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	ownerID := uuid.New()
	createTestUser(t, ds, ownerID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)

	theirs, err := ds.CreateMemoryFromInput(ctx, otherUserID, models.CreateMemoryInput{Content: "not yours", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	mine, err := ds.CreateMemoryFromInput(ctx, ownerID, models.CreateMemoryInput{Content: "mine", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	result, err := ds.DeleteMemoriesBatch(ctx, ownerID, models.BatchDeleteMemoryInput{
		IDs:       []uuid.UUID{theirs.ID, mine.ID},
		AllOrNone: false,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.DeletedCount, "wrong-owner id is skipped like a missing one in partial mode")

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(theirs.ID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists, "a delete request from a non-owner must never remove someone else's memory")

	exists, err = ds.dbClient.Memory.Query().Where(entmemory.ID(mine.ID)).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestDeleteMemoriesBatch_AllOrNoneWrongOwnerRollsBack(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	ownerID := uuid.New()
	createTestUser(t, ds, ownerID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)

	theirs, err := ds.CreateMemoryFromInput(ctx, otherUserID, models.CreateMemoryInput{Content: "not yours", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	mine, err := ds.CreateMemoryFromInput(ctx, ownerID, models.CreateMemoryInput{Content: "mine", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	_, err = ds.DeleteMemoriesBatch(ctx, ownerID, models.BatchDeleteMemoryInput{
		IDs:       []uuid.UUID{mine.ID, theirs.ID},
		AllOrNone: true,
	})
	require.ErrorIs(t, err, ErrMemoryNotFound)

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(mine.ID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists, "all_or_none must roll back the whole transaction, including ids that would have succeeded")

	exists, err = ds.dbClient.Memory.Query().Where(entmemory.ID(theirs.ID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists)
}

// --- PatchMemoriesBatch: this datastore method had no coverage beyond the
// too-many-ids guard before this file. ---

func TestPatchMemoriesBatch_EmptyIDsNoOp(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	result, err := ds.PatchMemoriesBatch(ctx, uuid.New(), models.BatchPatchMemoryInput{})
	require.NoError(t, err)
	require.Equal(t, 0, result.UpdatedCount)
	require.Empty(t, result.Results)
}

func TestPatchMemoriesBatch_HappyPathMultiple(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	first, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "one", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	second, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "two", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	starred := true
	result, err := ds.PatchMemoriesBatch(ctx, userID, models.BatchPatchMemoryInput{
		IDs:   []uuid.UUID{first.ID, second.ID},
		Patch: models.MemoryPatch{Starred: &starred},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.UpdatedCount)
	require.Len(t, result.Results, 2)
	for _, mem := range result.Results {
		require.True(t, mem.Starred)
	}

	reloaded, err := ds.GetMemory(ctx, userID, first.ID)
	require.NoError(t, err)
	require.True(t, reloaded.Starred, "batch patch must persist, not just echo back the requested change")
}

func TestPatchMemoriesBatch_AllOrNoneAbortsOnMissingButKeepsEarlierWrites(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	kept, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "original", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	newContent := "patched before the batch failed"
	result, err := ds.PatchMemoriesBatch(ctx, userID, models.BatchPatchMemoryInput{
		IDs:       []uuid.UUID{kept.ID, uuid.New()},
		Patch:     models.MemoryPatch{Content: &newContent},
		AllOrNone: true,
	})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, result)

	// Each id's UpdateMemory call is its own transaction, so all_or_none for
	// patch does NOT roll back ids that already succeeded before the failure —
	// unlike DeleteMemoriesBatch's all_or_none, which shares one transaction.
	reloaded, err := ds.GetMemory(ctx, userID, kept.ID)
	require.NoError(t, err)
	require.Equal(t, newContent, reloaded.Content, "already-patched rows are kept even though the batch call reports an error")
}

func TestPatchMemoriesBatch_PartialSkipsMissingAndWrongOwnerContinues(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)

	first, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "one", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	theirs, err := ds.CreateMemoryFromInput(ctx, otherUserID, models.CreateMemoryInput{Content: "not yours", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	second, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "two", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	starred := true
	result, err := ds.PatchMemoriesBatch(ctx, userID, models.BatchPatchMemoryInput{
		IDs:       []uuid.UUID{first.ID, uuid.New(), theirs.ID, second.ID},
		Patch:     models.MemoryPatch{Starred: &starred},
		AllOrNone: false,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.UpdatedCount, "missing id and wrong-owner id are both skipped like ErrMemoryNotFound")

	gotIDs := []uuid.UUID{result.Results[0].ID, result.Results[1].ID}
	require.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, gotIDs)

	otherReloaded, err := ds.GetMemory(ctx, otherUserID, theirs.ID)
	require.NoError(t, err)
	require.False(t, otherReloaded.Starred, "a patch request from a non-owner must never modify someone else's memory")
}

func TestPatchMemoriesBatch_PartialPropagatesNonNotFoundError(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	ownPersonalityID := uuid.New()
	createTestPersonality(t, ds, ownPersonalityID, userID)
	foreignPersonalityID := uuid.New()
	createTestPersonality(t, ds, foreignPersonalityID, otherUserID)

	// Personality-level memories so the shared patch's pinned_personality_id
	// clears validateLevelInput and reaches the ownership check.
	first, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "one", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &ownPersonalityID})
	require.NoError(t, err)
	second, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "two", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &ownPersonalityID})
	require.NoError(t, err)

	// A patch that fails validation (here: re-pinning to a personality the
	// caller doesn't own) aborts the whole partial batch immediately — it is
	// not silently skipped the way ErrMemoryNotFound is.
	result, err := ds.PatchMemoriesBatch(ctx, userID, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{first.ID, second.ID},
		Patch: models.MemoryPatch{
			SetPinnedPersonalityID: true,
			PinnedPersonalityID:    &foreignPersonalityID,
		},
		AllOrNone: false,
	})
	require.ErrorIs(t, err, ErrPersonalityNotFound)
	require.Nil(t, result)

	reloaded, err := ds.GetMemory(ctx, userID, second.ID)
	require.NoError(t, err)
	require.Equal(t, ownPersonalityID, *reloaded.PinnedPersonalityID, "the batch must stop at the first failing id rather than continuing to the rest")
}

func TestPatchMemoriesBatch_AllOrNonePropagatesNonNotFoundError(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	ownPersonalityID := uuid.New()
	createTestPersonality(t, ds, ownPersonalityID, userID)
	foreignPersonalityID := uuid.New()
	createTestPersonality(t, ds, foreignPersonalityID, otherUserID)

	first, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "one", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &ownPersonalityID})
	require.NoError(t, err)

	result, err := ds.PatchMemoriesBatch(ctx, userID, models.BatchPatchMemoryInput{
		IDs: []uuid.UUID{first.ID},
		Patch: models.MemoryPatch{
			SetPinnedPersonalityID: true,
			PinnedPersonalityID:    &foreignPersonalityID,
		},
		AllOrNone: true,
	})
	require.ErrorIs(t, err, ErrPersonalityNotFound)
	require.Nil(t, result)
}
