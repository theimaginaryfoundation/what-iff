package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestCommentFileID_ChatCopiesShareTheOriginal(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	persona := createFATestPersonality(t, ds, userID, "Scribe")

	original, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name: "plan.md", FileType: "text/markdown", S3Key: "users/u/agent/v1_plan.md",
		Origin: models.FileOriginAgent, CreatedByPersonalityID: &persona,
	})
	require.NoError(t, err)
	copy1, err := ds.CreateFileAttachmentReference(ctx, userID, original.ID)
	require.NoError(t, err)
	require.Equal(t, &original.ID, copy1.OriginalID, "a chat copy links to its original")
	copy2, err := ds.CreateFileAttachmentReference(ctx, userID, copy1.ID)
	require.NoError(t, err)
	require.Equal(t, &original.ID, copy2.OriginalID, "a copy of a copy links to the same original")
	shown, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name: "plan.md", FileType: "text/markdown", S3Key: original.S3Key, ContextOnly: true,
	})
	require.NoError(t, err)

	for _, id := range []uuid.UUID{original.ID, copy1.ID, copy2.ID} {
		got, err := ds.CommentFileID(ctx, userID, id)
		require.NoError(t, err)
		require.Equal(t, original.ID, got)
	}
	got, err := ds.CommentFileID(ctx, userID, shown.ID)
	require.NoError(t, err)
	require.Equal(t, shown.ID, got, "a context-only row is its own file")
	_, err = ds.CommentFileID(ctx, createFATestUser(t, ds), copy1.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)

	// An agent edit moves the copies with the original, not the context-only row.
	ok, err := ds.SwapAgentFileObject(ctx, userID, original.ID, original.S3Key, "users/u/agent/v2_plan.md", 3, "abc")
	require.NoError(t, err)
	require.True(t, ok)
	for _, id := range []uuid.UUID{copy1.ID, copy2.ID} {
		row, err := ds.GetFileAttachment(ctx, userID, id)
		require.NoError(t, err)
		require.Equal(t, "users/u/agent/v2_plan.md", row.S3Key)
	}
	row, err := ds.GetFileAttachment(ctx, userID, shown.ID)
	require.NoError(t, err)
	require.Equal(t, original.S3Key, row.S3Key)

	// Deleting the original hands it, and its comments, to the earliest copy.
	_, err = ds.PutFileCommentSet(ctx, userID, original.ID, models.FileCommentSet{Threads: []models.FileCommentThread{{ID: "t1"}}, BaselineKey: "users/u/agent/v2_plan.md"}, 0)
	require.NoError(t, err)
	require.NoError(t, ds.DeleteFileAttachment(ctx, userID, original.ID))
	for _, id := range []uuid.UUID{copy1.ID, copy2.ID} {
		got, err = ds.CommentFileID(ctx, userID, id)
		require.NoError(t, err)
		require.Equal(t, copy1.ID, got)
	}
	set, err := ds.GetFileCommentSet(ctx, userID, copy1.ID)
	require.NoError(t, err)
	require.NotNil(t, set)
	require.Len(t, set.Threads, 1)
}

func TestCommentFileID_BackfillsTheLinkFromTheSharedObject(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)

	// Rows made before original_id existed: the same object, no link.
	first, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain", S3Key: "users/u/a.txt"})
	require.NoError(t, err)
	later, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain", S3Key: "users/u/a.txt"})
	require.NoError(t, err)
	require.Nil(t, later.OriginalID)

	got, err := ds.CommentFileID(ctx, userID, later.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got, "the earliest row on the object is the original")
	row, err := ds.GetFileAttachment(ctx, userID, later.ID)
	require.NoError(t, err)
	require.Equal(t, &first.ID, row.OriginalID, "the inferred link is stored")
	got, err = ds.CommentFileID(ctx, userID, first.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got)
}
