package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// TestPersonalityPromptChangesAreAppendOnly pins issue #42's durable behavior:
// changing a personality's system prompt must create an immutable before/after
// audit item, and restoring an older prompt must append another item rather than
// rewriting or deleting the original history.
func TestPersonalityPromptChangesAreAppendOnly(t *testing.T) {
	ds, cleanup := newPersonalityPromptHistoryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	personalityID := uuid.New()
	insertPersonalityPromptHistoryFixture(t, ds, userID, personalityID, "prompt v1")

	current, err := ds.GetPersonality(ctx, userID, personalityID)
	require.NoError(t, err)
	current.SystemPrompt = "prompt v2"
	_, err = ds.UpdatePersonality(ctx, userID, *current)
	require.NoError(t, err)

	changes, err := ds.ListPersonalityPromptChanges(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "prompt v1", changes[0].OldPrompt)
	require.Equal(t, "prompt v2", changes[0].NewPrompt)
	require.Equal(t, models.PersonalityPromptChangeActionEdit, changes[0].Action)

	_, err = ds.RevertPersonalityPromptChange(ctx, userID, personalityID, changes[0].ID)
	require.NoError(t, err)

	restored, err := ds.GetPersonality(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Equal(t, "prompt v1", restored.SystemPrompt)

	changes, err = ds.ListPersonalityPromptChanges(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Len(t, changes, 2, "revert must append history instead of mutating it")
	require.Equal(t, "prompt v2", changes[0].OldPrompt)
	require.Equal(t, "prompt v1", changes[0].NewPrompt)
	require.Equal(t, models.PersonalityPromptChangeActionRevert, changes[0].Action)
	require.Equal(t, "prompt v1", changes[1].OldPrompt)
	require.Equal(t, "prompt v2", changes[1].NewPrompt)
}
