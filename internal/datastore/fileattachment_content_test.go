package datastore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestSwapFileObject_VersionCheckAndSharedRows(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	other := createFATestUser(t, ds)

	orig, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "notes.md", FileType: "text/markdown", S3Key: "users/u/files/v1_notes.md", Folder: "docs"})
	require.NoError(t, err)
	require.Equal(t, 1, orig.ContentVersion, "a new row starts at version 1")
	copyRow, err := ds.CreateFileAttachmentReference(ctx, userID, orig.ID)
	require.NoError(t, err)
	chatID := createFATestChat(t, ds, userID, uuid.Nil)
	msgID := createFATestChatMessage(t, ds, chatID)
	ctxRow, err := ds.CreateContextReference(ctx, userID, orig.ID, msgID)
	require.NoError(t, err)
	_, err = ds.CreateFileAttachment(ctx, other, models.FileAttachment{Name: "notes.md", FileType: "text/markdown", S3Key: "users/u/files/v1_notes.md"})
	require.NoError(t, err, "another owner's row on the same key is never touched")

	stale := 7
	ok, err := ds.SwapFileObject(ctx, userID, orig.ID, orig.S3Key, "users/u/files/v2_notes.md", SwapFileObjectOptions{ExpectVersion: &stale})
	require.NoError(t, err)
	require.False(t, ok, "a stale If-Match version does not swap")
	ok, err = ds.SwapFileObject(ctx, userID, orig.ID, "users/u/files/wrong.md", "users/u/files/v2_notes.md", SwapFileObjectOptions{})
	require.NoError(t, err)
	require.False(t, ok, "a stale old key does not swap")
	ok, err = ds.SwapFileObject(ctx, userID, orig.ID, orig.S3Key, "users/u/files/v2_notes.md", SwapFileObjectOptions{AgentOnly: true})
	require.NoError(t, err)
	require.False(t, ok, "AgentOnly refuses a user upload")

	v1 := 1
	ok, err = ds.SwapFileObject(ctx, userID, orig.ID, orig.S3Key, "users/u/files/v2_notes.md", SwapFileObjectOptions{
		ExpectVersion: &v1, SizeBytes: 42, SHA256: "abc", MoveSharedRows: true,
	})
	require.NoError(t, err)
	require.True(t, ok)

	got, err := ds.GetFileAttachment(ctx, userID, orig.ID)
	require.NoError(t, err)
	require.Equal(t, "users/u/files/v2_notes.md", got.S3Key)
	require.Equal(t, 2, got.ContentVersion)
	require.Equal(t, "abc", got.ContentSHA256)
	require.NotNil(t, got.SizeBytes)
	require.EqualValues(t, 42, *got.SizeBytes)
	require.NotNil(t, got.UpdatedAt)

	gotCopy, err := ds.GetFileAttachment(ctx, userID, copyRow.ID)
	require.NoError(t, err)
	require.Equal(t, "users/u/files/v2_notes.md", gotCopy.S3Key, "the chat copy moves with the file")
	require.Equal(t, 2, gotCopy.ContentVersion)
	gotCtx, err := ds.GetFileAttachment(ctx, userID, ctxRow.ID)
	require.NoError(t, err)
	require.Equal(t, "users/u/files/v1_notes.md", gotCtx.S3Key, "a context-only row keeps what the reply saw")
	theirs, err := ds.FindFileAttachmentsAtPath(ctx, other, "", "notes.md")
	require.NoError(t, err)
	require.Len(t, theirs, 1)
	require.Equal(t, "users/u/files/v1_notes.md", theirs[0].S3Key)

	page, err := ds.ListFileAttachments(ctx, userID, 1, 50, models.FileAttachmentFilters{ExcludeReferenceCopies: true, Kind: models.GalleryKindFiles})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount, "the copy still hides behind the original in the gallery")
	require.Equal(t, orig.ID, page.Results[0].(*models.FileAttachment).ID)
}

// createFileChunkTestSchema adds the file_chunks table the SQLite fixtures lack (chunk tests
// otherwise use sqlmock); the embedding column is a blob here, since SQLite has no vector type.
func createFileChunkTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS file_chunks (
		id uuid PRIMARY KEY,
		content text NOT NULL,
		embedding blob,
		sequence integer NOT NULL,
		metadata json,
		created_at datetime NOT NULL,
		file_chunk_file_attachment uuid
	)`)
	require.NoError(t, err)
}

func TestClearFileChunks_ResetsStatusSoChunkingRunsAgain(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createFileAttachmentTestSchema, alterChatsTableForAgentJobTests, createFileChunkTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	att, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "a.txt", FileType: "text/plain", S3Key: "k"})
	require.NoError(t, err)
	require.NoError(t, ds.SetFileChunkStatus(ctx, att.ID, fileattachment.ChunkStatusChunked))
	err = ds.CreateFileChunks(ctx, att.ID, []FileChunkInput{{Content: "x", Sequence: 0}})
	require.ErrorIs(t, err, ErrAlreadyChunked)

	require.NoError(t, ds.ClearFileChunks(ctx, att.ID))
	got, err := ds.GetFileAttachment(ctx, userID, att.ID)
	require.NoError(t, err)
	require.Equal(t, "", got.ChunkStatus)
}

func TestUpdateFileAttachmentName_Hardening(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)

	a, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "report.md", FileType: "text/markdown", S3Key: "users/u/files/a.md", Folder: "docs"})
	require.NoError(t, err)
	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "Taken.md", FileType: "text/markdown", S3Key: "users/u/files/b.md", Folder: "docs"})
	require.NoError(t, err)
	copyRow, err := ds.CreateFileAttachmentReference(ctx, userID, a.ID)
	require.NoError(t, err)

	_, err = ds.UpdateFileAttachmentName(ctx, userID, a.ID, "taken.MD")
	require.ErrorIs(t, err, ErrFileAttachmentPathTaken, "case-insensitive clash in the same folder")
	_, err = ds.UpdateFileAttachmentName(ctx, userID, a.ID, "bad/name.md")
	require.ErrorIs(t, err, models.ErrInvalidFileName)

	// A new extension the allowlist knows changes the type; the chat copy follows.
	renamed, err := ds.UpdateFileAttachmentName(ctx, userID, a.ID, "report.txt")
	require.NoError(t, err)
	require.Equal(t, "report.txt", renamed.Name)
	require.Equal(t, "text/plain", renamed.FileType)
	require.Nil(t, renamed.UpdatedAt, "a rename is not a content change")
	gotCopy, err := ds.GetFileAttachment(ctx, userID, copyRow.ID)
	require.NoError(t, err)
	require.Equal(t, "report.txt", gotCopy.Name)
	require.Equal(t, "text/plain", gotCopy.FileType)

	// No extension keeps the current one; an unknown extension keeps the type.
	renamed, err = ds.UpdateFileAttachmentName(ctx, userID, a.ID, "summary")
	require.NoError(t, err)
	require.Equal(t, "summary.txt", renamed.Name)
	renamed, err = ds.UpdateFileAttachmentName(ctx, userID, a.ID, "summary.weird")
	require.NoError(t, err)
	require.Equal(t, "text/plain", renamed.FileType)

	// An image stays an image whatever the new extension says.
	img, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "pic.png", FileType: "image/png", S3Key: "users/u/images/pic.png"})
	require.NoError(t, err)
	renamed, err = ds.UpdateFileAttachmentName(ctx, userID, img.ID, "pic.txt")
	require.NoError(t, err)
	require.Equal(t, "image/png", renamed.FileType)
	renamed, err = ds.UpdateFileAttachmentName(ctx, userID, img.ID, "pic.jpg")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", renamed.FileType)

	// Skill bundle files are refused.
	skill, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "SKILL.md", FileType: "text/markdown", S3Key: "users/u/skills/s/x.md", Folder: "skills/demo/references", Origin: models.FileOriginSkill, SkillID: ptrUUID(uuid.New())})
	if err == nil {
		_, err = ds.UpdateFileAttachmentName(ctx, userID, skill.ID, "other.md")
		require.ErrorIs(t, err, ErrSkillFileImmutable)
	}
}
