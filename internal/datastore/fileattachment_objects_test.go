package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

func createFAObjectRow(t *testing.T, ds *Datastore, userID uuid.UUID, att models.FileAttachment) *models.FileAttachment {
	t.Helper()
	created, err := ds.CreateFileAttachment(context.Background(), userID, att)
	require.NoError(t, err)
	return created
}

func TestReferencedFileAttachmentKeys(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	otherID := createFATestUser(t, ds)
	createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "a.png", FileType: "image/png", S3Key: "users/u/images/a.png"})
	createFAObjectRow(t, ds, otherID, models.FileAttachment{Name: "b.png", FileType: "image/png", S3Key: "users/o/images/b.png"})

	got, err := ds.ReferencedFileAttachmentKeys(ctx, []string{"users/u/images/a.png", "users/o/images/b.png", "users/u/images/gone.png", "", "users/u/images/a.png"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"users/u/images/a.png": true, "users/o/images/b.png": true}, got,
		"any owner's row counts; unknown and empty keys do not")

	got, err = ds.ReferencedFileAttachmentKeys(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestListFileAttachmentObjectRefs_Scopes(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	otherID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	otherChatID := createFATestChat(t, ds, userID, modelID)
	msgID := createFATestChatMessage(t, ds, chatID)
	otherMsgID := createFATestChatMessage(t, ds, otherChatID)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")

	inChat := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "c.txt", FileType: "text/plain", ChatMessageID: &msgID, S3Key: "k/chat"})
	createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "o.txt", FileType: "text/plain", ChatMessageID: &otherMsgID, S3Key: "k/other-chat"})
	inPersonality := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "p.md", FileType: "text/markdown", PersonalityID: &personalityID})
	createFAObjectRow(t, ds, otherID, models.FileAttachment{Name: "x.png", FileType: "image/png"})

	chatRefs, err := ds.ListChatFileAttachmentObjectRefs(ctx, userID, chatID)
	require.NoError(t, err)
	require.Len(t, chatRefs, 1)
	require.Equal(t, inChat.ID, chatRefs[0].ID)
	require.Equal(t, "k/chat", chatRefs[0].S3Key)
	require.NotNil(t, chatRefs[0].ChatID, "chat id is resolved for legacy key derivation")
	require.Equal(t, chatID, *chatRefs[0].ChatID)

	// Another user cannot list this chat's rows.
	none, err := ds.ListChatFileAttachmentObjectRefs(ctx, otherID, chatID)
	require.NoError(t, err)
	require.Empty(t, none)

	pRefs, err := ds.ListPersonalityFileAttachmentObjectRefs(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Len(t, pRefs, 1)
	require.Equal(t, inPersonality.ID, pRefs[0].ID)
	require.NotNil(t, pRefs[0].PersonalityID)

	all, err := ds.ListUserFileAttachmentObjectRefs(ctx, userID)
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func TestFileAttachmentProviderFileShared(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	fileID := "file-abc"
	orig := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "a.png", FileType: "image/png", FileID: &fileID, S3Key: "users/u/images/a.png"})

	shared, err := ds.FileAttachmentProviderFileShared(ctx, orig.ID, fileID)
	require.NoError(t, err)
	require.False(t, shared)

	ref, err := ds.CreateFileAttachmentReference(ctx, userID, orig.ID)
	require.NoError(t, err)
	shared, err = ds.FileAttachmentProviderFileShared(ctx, orig.ID, fileID)
	require.NoError(t, err)
	require.True(t, shared, "the reference copy carries the same provider file")
	shared, err = ds.FileAttachmentProviderFileShared(ctx, ref.ID, fileID)
	require.NoError(t, err)
	require.True(t, shared)

	shared, err = ds.FileAttachmentProviderFileShared(ctx, orig.ID, "")
	require.NoError(t, err)
	require.False(t, shared)
}

// TestChatCascade_ReleasesUnreferencedObjects runs the delete-chat sequence against a real
// datastore and the local file store: read the chat's rows, remove them (the test schema has no
// FK cascade, so the cascade is done by hand), then release. The object a reference copy in
// another chat still uses survives; everything else goes.
func TestChatCascade_ReleasesUnreferencedObjects(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	t.Setenv("LOCAL_FILE_STORE_DIR", t.TempDir())
	fs, err := storage.NewFileStore(ctx, "", "", zap.NewNop())
	require.NoError(t, err)

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	otherChatID := createFATestChat(t, ds, userID, modelID)
	msgID := createFATestChatMessage(t, ds, chatID)
	otherMsgID := createFATestChatMessage(t, ds, otherChatID)

	doc := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "d.txt", FileType: "text/plain", ChatMessageID: &msgID})
	docKey := storage.FileKeyForChat(userID, chatID, doc.ID, doc.Name)
	require.NoError(t, ds.SetFileAttachmentS3Key(ctx, userID, doc.ID, docKey))
	img := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "i.png", FileType: "image/png", ChatMessageID: &msgID})
	imgKey := storage.FileKeyForImage(userID, img.ID, img.Name)
	require.NoError(t, ds.SetFileAttachmentS3Key(ctx, userID, img.ID, imgKey))
	// Make img's row the older one so the reference copy below is unambiguous.
	_, err = ds.sqlDB.Exec(`UPDATE file_attachments SET created_at = ? WHERE id = ?`, time.Now().Add(-time.Hour), img.ID.String())
	require.NoError(t, err)
	ref, err := ds.CreateFileAttachmentReference(ctx, userID, img.ID)
	require.NoError(t, err)
	_, err = ds.dbClient.FileAttachment.UpdateOneID(ref.ID).SetChatMessageID(otherMsgID).Save(ctx)
	require.NoError(t, err)

	for _, k := range []string{docKey, imgKey, storage.FileKeyForImageThumbnail(userID, img.ID)} {
		require.NoError(t, fs.UploadFile(ctx, k, []byte(k), "application/octet-stream"))
	}

	refs, err := ds.ListChatFileAttachmentObjectRefs(ctx, userID, chatID)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	for _, r := range refs {
		require.NoError(t, ds.DeleteFileAttachment(ctx, userID, r.ID))
	}

	res := storage.ReleaseAttachmentObjects(ctx, zap.NewNop(), fs, ds, userID, refs)
	require.Equal(t, storage.ReleaseResult{Deleted: 2, Kept: 1}, res)

	has := func(k string) bool {
		b, err := fs.DownloadFile(ctx, k)
		require.NoError(t, err)
		return b != nil
	}
	require.False(t, has(docKey))
	require.False(t, has(storage.FileKeyForImageThumbnail(userID, img.ID)))
	require.True(t, has(imgKey), "the reference copy in the other chat still uses it")
}

func TestExistingFileAttachmentIDs(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	kept := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain"})
	gone := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "b.txt", FileType: "text/plain"})
	require.NoError(t, ds.DeleteFileAttachment(ctx, userID, gone.ID))

	got, err := ds.ExistingFileAttachmentIDs(ctx, []uuid.UUID{kept.ID, gone.ID, uuid.New()})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{kept.ID: true}, got)
}

// TestRelease_CascadeThatDidNotFireKeepsObjects: if the listed rows are still there after the
// "delete" (an FK cascade that did not fire), nothing of theirs is released.
func TestRelease_CascadeThatDidNotFireKeepsObjects(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	t.Setenv("LOCAL_FILE_STORE_DIR", t.TempDir())
	fs, err := storage.NewFileStore(ctx, "", "", zap.NewNop())
	require.NoError(t, err)

	userID := createFATestUser(t, ds)
	chatID := createFATestChat(t, ds, userID, createFATestModel(t, ds))
	msgID := createFATestChatMessage(t, ds, chatID)
	img := createFAObjectRow(t, ds, userID, models.FileAttachment{Name: "i.png", FileType: "image/png", ChatMessageID: &msgID})
	thumb := storage.FileKeyForImageThumbnail(userID, img.ID)
	legacy := storage.FileKeyForImage(userID, img.ID, img.Name) // no s3_key: a derived key
	for _, k := range []string{thumb, legacy} {
		require.NoError(t, fs.UploadFile(ctx, k, []byte(k), "image/png"))
	}

	refs, err := ds.ListChatFileAttachmentObjectRefs(ctx, userID, chatID)
	require.NoError(t, err)
	require.Len(t, refs, 1)

	// No delete happens: the row survives.
	res := storage.ReleaseAttachmentObjects(ctx, zap.NewNop(), fs, ds, userID, refs)
	require.Equal(t, storage.ReleaseResult{Kept: 1}, res)
	for _, k := range []string{thumb, legacy} {
		b, err := fs.DownloadFile(ctx, k)
		require.NoError(t, err)
		require.NotNil(t, b, k)
	}
}
