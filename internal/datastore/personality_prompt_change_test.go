package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/personalitypromptchange"
)

func createPersonalityPromptChangeTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE personality_prompt_changes (
		id uuid PRIMARY KEY,
		user_id uuid NOT NULL,
		personality_id uuid NOT NULL,
		old_prompt text NOT NULL,
		new_prompt text NOT NULL,
		action text NOT NULL,
		reverted_change_id uuid,
		created_at datetime NOT NULL
	)`)
	require.NoError(t, err)
}

func newPersonalityPromptChangeTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createPersonalityPromptChangeTestSchema)
}

func insertPromptChangeTestRow(
	t *testing.T,
	ds *Datastore,
	userID, personalityID uuid.UUID,
	oldPrompt, newPrompt string,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.PersonalityPromptChange.Create().
		SetID(id).
		SetUserID(userID).
		SetPersonalityID(personalityID).
		SetOldPrompt(oldPrompt).
		SetNewPrompt(newPrompt).
		SetAction(personalitypromptchange.ActionEdit).
		SetCreatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func TestListPersonalityPromptChangesIsOwnerScopedAndNewestFirst(t *testing.T) {
	ds, cleanup := newPersonalityPromptChangeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ownerID := uuid.New()
	otherUserID := uuid.New()
	personalityID := uuid.New()
	otherPersonalityID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, ownerID))
	require.NoError(t, insertMemoryMergeTestUser(t, ds, otherUserID))
	insertCompactionTestPersonality(t, ds, ownerID, personalityID, "")
	insertCompactionTestPersonality(t, ds, otherUserID, otherPersonalityID, "")

	now := time.Now().UTC()
	olderID := insertPromptChangeTestRow(t, ds, ownerID, personalityID, "one", "two", now.Add(-time.Minute))
	newerID := insertPromptChangeTestRow(t, ds, ownerID, personalityID, "two", "three", now)
	insertPromptChangeTestRow(t, ds, otherUserID, otherPersonalityID, "private", "still private", now.Add(time.Minute))

	changes, err := ds.ListPersonalityPromptChanges(ctx, ownerID, personalityID)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	require.Equal(t, newerID, changes[0].ID)
	require.Equal(t, olderID, changes[1].ID)
	for _, change := range changes {
		require.Equal(t, ownerID, change.UserID)
		require.Equal(t, personalityID, change.PersonalityID)
	}

	_, err = ds.ListPersonalityPromptChanges(ctx, otherUserID, personalityID)
	require.ErrorIs(t, err, ErrPersonalityNotFound)
}

func TestRevertPersonalityPromptChangeRestoresPromptAndAppendsAuditEvent(t *testing.T) {
	ds, cleanup := newPersonalityPromptChangeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	personalityID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	insertCompactionTestPersonality(t, ds, userID, personalityID, "")

	_, err := ds.dbClient.Personality.UpdateOneID(personalityID).SetSystemPrompt("prompt v2").Save(ctx)
	require.NoError(t, err)
	originalID := insertPromptChangeTestRow(t, ds, userID, personalityID, "prompt v1", "prompt v2", time.Now().UTC())

	revert, err := ds.RevertPersonalityPromptChange(ctx, userID, personalityID, originalID)
	require.NoError(t, err)
	require.Equal(t, personalitypromptchange.ActionRevert.String(), string(revert.Action))
	require.Equal(t, "prompt v2", revert.OldPrompt)
	require.Equal(t, "prompt v1", revert.NewPrompt)
	require.NotNil(t, revert.RevertedChangeID)
	require.Equal(t, originalID, *revert.RevertedChangeID)

	updated, err := ds.dbClient.Personality.Query().Where(personality.ID(personalityID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "prompt v1", updated.SystemPrompt)

	changes, err := ds.ListPersonalityPromptChanges(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	require.Equal(t, revert.ID, changes[0].ID)
	require.Equal(t, originalID, changes[1].ID)
}

func TestRevertPersonalityPromptChangeRejectsAnotherUser(t *testing.T) {
	ds, cleanup := newPersonalityPromptChangeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ownerID := uuid.New()
	otherUserID := uuid.New()
	personalityID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, ownerID))
	require.NoError(t, insertMemoryMergeTestUser(t, ds, otherUserID))
	insertCompactionTestPersonality(t, ds, ownerID, personalityID, "")

	changeID := insertPromptChangeTestRow(t, ds, ownerID, personalityID, "prompt v1", "prompt v2", time.Now().UTC())

	_, err := ds.RevertPersonalityPromptChange(ctx, otherUserID, personalityID, changeID)
	require.ErrorIs(t, err, ErrPersonalityPromptChangeNotFound)
}

func TestRevertPersonalityPromptChangeIsIdempotentWhenAlreadyRestored(t *testing.T) {
	ds, cleanup := newPersonalityPromptChangeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	personalityID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	insertCompactionTestPersonality(t, ds, userID, personalityID, "")

	changeID := insertPromptChangeTestRow(t, ds, userID, personalityID, "system", "prompt v2", time.Now().UTC())

	returned, err := ds.RevertPersonalityPromptChange(ctx, userID, personalityID, changeID)
	require.NoError(t, err)
	require.Equal(t, changeID, returned.ID)

	changes, err := ds.ListPersonalityPromptChanges(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Len(t, changes, 1, "an already-restored prompt must not append a duplicate revert event")
}
