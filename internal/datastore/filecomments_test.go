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

func TestFileEditGrants_ForTheChatAndTheWholeFile(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID, other := createFATestUser(t, ds), createFATestUser(t, ds)
	chat, err := ds.dbClient.Chat.Create().SetName("c").SetOwnerID(userID).Save(ctx)
	require.NoError(t, err)
	otherChat, err := ds.dbClient.Chat.Create().SetName("o").SetOwnerID(userID).Save(ctx)
	require.NoError(t, err)
	foreign, err := ds.dbClient.Chat.Create().SetName("f").SetOwnerID(other).Save(ctx)
	require.NoError(t, err)

	file, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "brief.md", FileType: "text/markdown", S3Key: "users/u/brief.md"})
	require.NoError(t, err)
	copied, err := ds.CreateFileAttachmentReference(ctx, userID, file.ID)
	require.NoError(t, err)

	// Granted through the chat copy: the file and every copy of it, in that chat only.
	require.NoError(t, ds.GrantFileEdit(ctx, userID, chat.ID, copied.ID))
	require.NoError(t, ds.GrantFileEdit(ctx, userID, chat.ID, file.ID), "again is a no-op")
	for _, id := range []uuid.UUID{file.ID, copied.ID} {
		ok, err := ds.HasFileEditGrant(ctx, userID, chat.ID, id)
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := ds.HasFileEditGrant(ctx, userID, otherChat.ID, file.ID)
	require.NoError(t, err)
	require.False(t, ok, "another chat's agent")

	// A chat holds grants on several files, and a file is granted to several chats.
	second, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "notes.md", FileType: "text/markdown", S3Key: "users/u/notes.md"})
	require.NoError(t, err)
	require.NoError(t, ds.GrantFileEdit(ctx, userID, chat.ID, second.ID))
	require.NoError(t, ds.GrantFileEdit(ctx, userID, otherChat.ID, file.ID))
	for _, g := range []struct{ chat, file uuid.UUID }{{chat.ID, file.ID}, {chat.ID, second.ID}, {otherChat.ID, file.ID}} {
		ok, err := ds.HasFileEditGrant(ctx, userID, g.chat, g.file)
		require.NoError(t, err)
		require.True(t, ok)
	}

	require.ErrorIs(t, ds.GrantFileEdit(ctx, userID, foreign.ID, file.ID), ErrChatNotFound)
	_, err = ds.HasFileEditGrant(ctx, other, chat.ID, file.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound, "someone else's file")
}
