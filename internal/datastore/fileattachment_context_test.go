package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A file the agent fetched to look at (find_context) is shown on the reply as a context-only
// reference: it shares the original's stored object, loads with the reply, and is never listed
// as a file of its own, even after the original is deleted.
func TestContextReferenceIsShownOnTheReplyButNeverListed(t *testing.T) {
	f := newFolderFixture(t)
	ctx := context.Background()
	chatID := createFATestChat(t, f.ds, f.user, createFATestModel(t, f.ds))
	replyID := createFATestChatMessageWithOrigin(t, f.ds, chatID, "Assistant")

	ref, err := f.ds.CreateContextReference(ctx, f.user, f.ids["art.png"], replyID)
	require.NoError(t, err)
	assert.True(t, ref.ContextOnly)
	assert.Equal(t, "k/art.png", ref.S3Key, "no new object: it shares the original's")
	assert.Equal(t, "art.png", ref.Name)
	assert.Equal(t, "", ref.Folder)

	loaded, err := f.ds.GetFileAttachment(ctx, f.user, ref.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.ChatMessageID)
	assert.Equal(t, replyID, *loaded.ChatMessageID, "the person sees the file on the reply")
	assert.True(t, loaded.ContextOnly, "loaded with its flag, so the relay can skip it")

	listed := func(filters models.FileAttachmentFilters) int {
		page, err := f.ds.ListFileAttachments(ctx, f.user, 1, 100, filters)
		require.NoError(t, err)
		n := 0
		for _, r := range page.Results {
			if r.(*models.FileAttachment).ID == ref.ID {
				n++
			}
		}
		return n
	}
	assert.Zero(t, listed(models.FileAttachmentFilters{}), "not in the agent's or the API's file list")
	assert.Zero(t, listed(models.FileAttachmentFilters{ExcludeReferenceCopies: true}), "not in the gallery")

	require.NoError(t, f.ds.DeleteFileAttachment(ctx, f.user, f.ids["art.png"]))
	assert.Zero(t, listed(models.FileAttachmentFilters{ExcludeReferenceCopies: true}),
		"deleting the original does not bring the reference into the gallery")
	folders, err := f.ds.ListGalleryFolders(ctx, f.user, models.GalleryKindAll)
	require.NoError(t, err)
	for _, fc := range folders {
		assert.NotEqual(t, "", fc.Path)
	}
	moved, err := f.ds.MoveFileAttachmentsToFolder(ctx, f.user, []uuid.UUID{ref.ID}, "x")
	require.NoError(t, err)
	assert.Zero(t, moved, "a context reference is not a gallery file to move")
}
