package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/filecommentset"
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

// grant drops GrantFileEdit's created flag.
func grant(_ bool, err error) error { return err }

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
	require.NoError(t, grant(ds.GrantFileEdit(ctx, userID, chat.ID, copied.ID)))
	created, err := ds.GrantFileEdit(ctx, userID, chat.ID, file.ID)
	require.NoError(t, err)
	require.False(t, created, "again is a no-op")
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
	require.NoError(t, grant(ds.GrantFileEdit(ctx, userID, chat.ID, second.ID)))
	require.NoError(t, grant(ds.GrantFileEdit(ctx, userID, otherChat.ID, file.ID)))
	for _, g := range []struct{ chat, file uuid.UUID }{{chat.ID, file.ID}, {chat.ID, second.ID}, {otherChat.ID, file.ID}} {
		ok, err := ds.HasFileEditGrant(ctx, userID, g.chat, g.file)
		require.NoError(t, err)
		require.True(t, ok)
	}

	_, err = ds.GrantFileEdit(ctx, userID, foreign.ID, file.ID)
	require.ErrorIs(t, err, ErrChatNotFound)
	_, err = ds.HasFileEditGrant(ctx, other, chat.ID, file.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound, "someone else's file")

	// Listed for the chat, newest first, and revoked through any row of the file.
	grants, err := ds.ListFileEditGrants(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	require.ElementsMatch(t, []string{"brief.md", "notes.md"}, []string{grants[0].FileName, grants[1].FileName})
	require.NoError(t, ds.RevokeFileEdit(ctx, userID, chat.ID, copied.ID))
	ok, err = ds.HasFileEditGrant(ctx, userID, chat.ID, file.ID)
	require.NoError(t, err)
	require.False(t, ok, "revoked")
	ok, err = ds.HasFileEditGrant(ctx, userID, otherChat.ID, file.ID)
	require.NoError(t, err)
	require.True(t, ok, "only for that chat")
	require.NoError(t, ds.RevokeFileEdit(ctx, userID, chat.ID, file.ID), "revoking again is a no-op")
	grants, err = ds.ListFileEditGrants(ctx, other, chat.ID)
	require.NoError(t, err)
	require.Empty(t, grants, "someone else's chat")
}

func TestFileEditGrants_AreForThePersonalityTheChatHad(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	vix := createFATestPersonality(t, ds, userID, "Vix")
	iris := createFATestPersonality(t, ds, userID, "Iris")
	chat, err := ds.dbClient.Chat.Create().SetName("c").SetOwnerID(userID).SetPersonalityID(vix).Save(ctx)
	require.NoError(t, err)
	file, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "brief.md", FileType: "text/markdown", S3Key: "users/u/brief.md"})
	require.NoError(t, err)

	created, err := ds.GrantFileEdit(ctx, userID, chat.ID, file.ID)
	require.NoError(t, err)
	require.True(t, created)
	has := func() bool {
		ok, err := ds.HasFileEditGrant(ctx, userID, chat.ID, file.ID)
		require.NoError(t, err)
		return ok
	}
	require.True(t, has())

	// Switching the chat to another personality does not pass the grant on.
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chat.ID).SetPersonalityID(iris).Exec(ctx))
	require.False(t, has())
	// Mentioning the new one grants it, and not the old one any more.
	_, err = ds.GrantFileEdit(ctx, userID, chat.ID, file.ID)
	require.NoError(t, err)
	require.True(t, has())
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chat.ID).SetPersonalityID(vix).Exec(ctx))
	require.False(t, has())
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chat.ID).ClearPersonality().Exec(ctx))
	require.False(t, has(), "nor the chat with no personality")
}

func TestFileCommentSet_StoredOnlyOverTheRevisionRead(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID, other := createFATestUser(t, ds), createFATestUser(t, ds)
	file, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "notes.md", FileType: "text/markdown", S3Key: "users/u/notes.md"})
	require.NoError(t, err)

	got, err := ds.GetFileCommentSet(ctx, userID, file.ID)
	require.NoError(t, err)
	require.Nil(t, got, "a file with no comments has no set")

	thread := models.FileCommentThread{ID: "t1", Quote: "plan", Comments: []models.FileComment{{ID: "t1", AuthorID: "user:1", AuthorName: "Ada", Text: "Why?"}}}
	rev, err := ds.PutFileCommentSet(ctx, userID, file.ID, models.FileCommentSet{Threads: []models.FileCommentThread{thread}, BaselineText: "the plan", BaselineKey: "users/u/notes.md"}, 0)
	require.NoError(t, err)
	require.Equal(t, 1, rev)
	_, err = ds.PutFileCommentSet(ctx, userID, file.ID, models.FileCommentSet{}, 0)
	require.ErrorIs(t, err, ErrFileCommentsRevision, "a second first write")

	got, err = ds.GetFileCommentSet(ctx, userID, file.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.Revision)
	require.Equal(t, "the plan", got.BaselineText)
	require.Equal(t, "users/u/notes.md", got.BaselineKey)
	require.Len(t, got.Threads, 1)
	require.Equal(t, "Why?", got.Threads[0].Comments[0].Text)

	rev, err = ds.PutFileCommentSet(ctx, userID, file.ID, models.FileCommentSet{BaselineText: "the plan", BaselineKey: "users/u/notes.md"}, 1)
	require.NoError(t, err)
	require.Equal(t, 2, rev)
	_, err = ds.PutFileCommentSet(ctx, userID, file.ID, models.FileCommentSet{}, 1)
	require.ErrorIs(t, err, ErrFileCommentsRevision, "a write over a revision since replaced")
	got, err = ds.GetFileCommentSet(ctx, userID, file.ID)
	require.NoError(t, err)
	require.Equal(t, 2, got.Revision)
	require.NotNil(t, got.Threads, "no threads are stored as an empty list")
	require.Empty(t, got.Threads)

	got, err = ds.GetFileCommentSet(ctx, other, file.ID)
	require.NoError(t, err)
	require.Nil(t, got, "someone else's file reads as having none")
	_, err = ds.PutFileCommentSet(ctx, other, file.ID, models.FileCommentSet{}, 2)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestHandOverCopiesOf_ACascadeKeepsTheCommentsWithACopy(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatA := createFATestChat(t, ds, userID, modelID)
	chatB := createFATestChat(t, ds, userID, modelID)
	msgA := createFATestChatMessage(t, ds, chatA)
	msgB := createFATestChatMessage(t, ds, chatB)

	// A file uploaded in chat A, used in chat B, and commented there.
	original, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.md", FileType: "text/markdown", S3Key: "users/u/a.md", ChatMessageID: &msgA})
	require.NoError(t, err)
	// A second row on the same object in chat A, made first, goes with it, so it can't be the heir.
	sameChat, err := ds.CreateFileAttachmentReference(ctx, userID, original.ID)
	require.NoError(t, err)
	require.NoError(t, ds.dbClient.FileAttachment.UpdateOneID(sameChat.ID).SetChatMessageID(msgA).Exec(ctx))
	ref, err := ds.CreateFileAttachmentReference(ctx, userID, original.ID)
	require.NoError(t, err)
	require.NoError(t, ds.dbClient.FileAttachment.UpdateOneID(ref.ID).SetChatMessageID(msgB).Exec(ctx))
	_, err = ds.PutFileCommentSet(ctx, userID, original.ID, models.FileCommentSet{Threads: []models.FileCommentThread{{ID: "t1"}}, BaselineKey: original.S3Key}, 0)
	require.NoError(t, err)

	// What DeleteChat does before the cascade; the test schema has no cascades, so the rows are
	// deleted here as the cascade would.
	doomed := entfileattachment.HasChatMessageWith(chatmessage.HasChatWith(entchat.ID(chatA)))
	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, handOverCopiesOf(ctx, tx, userID, doomed))
	_, err = tx.FileCommentSet.Delete().Where(filecommentset.HasFileAttachmentWith(doomed)).Exec(ctx)
	require.NoError(t, err)
	_, err = tx.FileAttachment.Delete().Where(doomed).Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	got, err := ds.CommentFileID(ctx, userID, ref.ID)
	require.NoError(t, err)
	require.Equal(t, ref.ID, got, "the copy in chat B is now the file, not the row in chat A")
	set, err := ds.GetFileCommentSet(ctx, userID, ref.ID)
	require.NoError(t, err)
	require.NotNil(t, set, "and it kept the comments")
	require.Len(t, set.Threads, 1)
}

func TestCommentFileID_FollowsACopyOfAnUnlinkedCopy(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)

	// first and unlinked share an object from before original_id; copy was made from unlinked.
	first, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain", S3Key: "users/u/a.txt"})
	require.NoError(t, err)
	unlinked, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain", S3Key: "users/u/a.txt"})
	require.NoError(t, err)
	copy1, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain", S3Key: "users/u/a.txt", OriginalID: &unlinked.ID})
	require.NoError(t, err)

	for _, id := range []uuid.UUID{copy1.ID, unlinked.ID, first.ID} {
		got, err := ds.CommentFileID(ctx, userID, id)
		require.NoError(t, err)
		require.Equal(t, first.ID, got, "one set for all three rows")
	}
}

// deleteAsCascade does what DeleteChat does for the chat's files, then deletes them as the
// foreign-key cascade would (the test schema has no cascades).
func deleteAsCascade(t *testing.T, ds *Datastore, userID, chatID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	doomed := entfileattachment.HasChatMessageWith(chatmessage.HasChatWith(entchat.ID(chatID)))
	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, handOverCopiesOf(ctx, tx, userID, doomed))
	_, err = tx.FileCommentSet.Delete().Where(filecommentset.HasFileAttachmentWith(doomed)).Exec(ctx)
	require.NoError(t, err)
	_, err = tx.FileAttachment.Delete().Where(doomed).Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestHandOverCopiesOf_RelinksCopiesBeforeAnyCommentAndAfter(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chats := []uuid.UUID{createFATestChat(t, ds, userID, modelID), createFATestChat(t, ds, userID, modelID), createFATestChat(t, ds, userID, modelID)}
	var msgs []uuid.UUID
	for _, c := range chats {
		msgs = append(msgs, createFATestChatMessage(t, ds, c))
	}
	original, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.md", FileType: "text/markdown", S3Key: "users/u/a.md", ChatMessageID: &msgs[0]})
	require.NoError(t, err)
	var copies []*models.FileAttachment
	for _, m := range msgs[1:] {
		c, err := ds.CreateFileAttachmentReference(ctx, userID, original.ID)
		require.NoError(t, err)
		require.NoError(t, ds.dbClient.FileAttachment.UpdateOneID(c.ID).SetChatMessageID(m).Exec(ctx))
		copies = append(copies, c)
	}

	// The original's chat goes before anyone comments: the copies are still one file.
	deleteAsCascade(t, ds, userID, chats[0])
	for _, c := range copies {
		got, err := ds.CommentFileID(ctx, userID, c.ID)
		require.NoError(t, err)
		require.Equal(t, copies[0].ID, got)
	}
	_, err = ds.PutFileCommentSet(ctx, userID, copies[0].ID, models.FileCommentSet{Threads: []models.FileCommentThread{{ID: "t1"}}, BaselineKey: original.S3Key}, 0)
	require.NoError(t, err)

	// Then the chat holding the new original goes: the last copy keeps the comments.
	deleteAsCascade(t, ds, userID, chats[1])
	got, err := ds.CommentFileID(ctx, userID, copies[1].ID)
	require.NoError(t, err)
	require.Equal(t, copies[1].ID, got)
	set, err := ds.GetFileCommentSet(ctx, userID, copies[1].ID)
	require.NoError(t, err)
	require.NotNil(t, set)
	require.Len(t, set.Threads, 1)
}

func TestHandOverCopies_ACopyHoldingCommentsAfterItsOriginalWentHandsThemOn(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	original, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.md", FileType: "text/markdown", S3Key: "users/u/a.md"})
	require.NoError(t, err)
	c1, err := ds.CreateFileAttachmentReference(ctx, userID, original.ID)
	require.NoError(t, err)
	c2, err := ds.CreateFileAttachmentReference(ctx, userID, original.ID)
	require.NoError(t, err)
	// The state the cascade used to leave: the original gone with no hand-over, and the copies
	// each holding comments written through them.
	require.NoError(t, ds.dbClient.FileAttachment.DeleteOneID(original.ID).Exec(ctx))
	thread := func(id string, start int) []models.FileCommentThread {
		return []models.FileCommentThread{{ID: id, Anchor: &models.FileCommentRange{Start: start, End: start + 2}, Quote: "hi"}}
	}
	_, err = ds.PutFileCommentSet(ctx, userID, c1.ID, models.FileCommentSet{Threads: thread("t1", 0), BaselineKey: "k"}, 0)
	require.NoError(t, err)
	_, err = ds.PutFileCommentSet(ctx, userID, c2.ID, models.FileCommentSet{Threads: thread("t2", 4), BaselineKey: "k"}, 0)
	require.NoError(t, err)

	require.NoError(t, ds.DeleteFileAttachment(ctx, userID, c1.ID))
	set, err := ds.GetFileCommentSet(ctx, userID, c2.ID)
	require.NoError(t, err)
	require.NotNil(t, set)
	require.Len(t, set.Threads, 2, "both sets, joined")
	require.Equal(t, "t2", set.Threads[0].ID)
	require.NotNil(t, set.Threads[0].Anchor, "the heir's own threads keep their places")
	require.Nil(t, set.Threads[1].Anchor, "the joined ones go on the whole file, quote kept")
	require.Equal(t, "hi", set.Threads[1].Quote)
}
