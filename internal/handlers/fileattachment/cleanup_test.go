package fileattachment

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// memRows is an in-memory attachment table: reference counting answers come from the rows that
// are still present, as they would from the datastore.
type memRows struct {
	rows map[uuid.UUID]models.FileAttachment
}

func newMemRows(rows ...models.FileAttachment) *memRows {
	m := &memRows{rows: map[uuid.UUID]models.FileAttachment{}}
	for _, r := range rows {
		m.rows[r.ID] = r
	}
	return m
}

func (m *memRows) ListFileAttachments(context.Context, uuid.UUID, int, int, models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *memRows) GetFileAttachment(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.FileAttachment, error) {
	r, ok := m.rows[id]
	if !ok {
		return nil, datastore.ErrFileAttachmentNotFound
	}
	return &r, nil
}

func (m *memRows) DeleteFileAttachment(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	delete(m.rows, id)
	return nil
}

func (m *memRows) FileAttachmentProviderFileShared(_ context.Context, id uuid.UUID, fileID string) (bool, error) {
	for rid, r := range m.rows {
		if rid != id && r.FileID != nil && *r.FileID == fileID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRows) ReferencedFileAttachmentKeys(_ context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, k := range keys {
		for _, r := range m.rows {
			if r.S3Key == k {
				out[k] = true
			}
		}
	}
	return out, nil
}

func (m *memRows) ExistingFileAttachmentIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		if _, ok := m.rows[id]; ok {
			out[id] = true
		}
	}
	return out, nil
}

func newLocalStore(t *testing.T) storage.FileStore {
	t.Helper()
	t.Setenv("LOCAL_FILE_STORE_DIR", t.TempDir())
	fs, err := storage.NewFileStore(context.Background(), "", "", zap.NewNop())
	require.NoError(t, err)
	return fs
}

func put(t *testing.T, fs storage.FileStore, key string) {
	t.Helper()
	require.NoError(t, fs.UploadFile(context.Background(), key, []byte("bytes of "+key), "application/octet-stream"))
}

func exists(t *testing.T, fs storage.FileStore, key string) bool {
	t.Helper()
	b, err := fs.DownloadFile(context.Background(), key)
	require.NoError(t, err)
	return len(b) > 0
}

func deleteRequest(userID, id uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/file-attachment/"+id.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	return mux.SetURLVars(req, map[string]string{"id": id.String()})
}

func TestDeleteFileAttachment_RemovesUnreferencedObjectAndThumbnail(t *testing.T) {
	fs := newLocalStore(t)
	userID := uuid.New()
	img := models.FileAttachment{ID: uuid.New(), Name: "cat.png", FileType: "image/png"}
	img.S3Key = storage.FileKeyForImage(userID, img.ID, img.Name)
	thumb := storage.FileKeyForImageThumbnail(userID, img.ID)
	put(t, fs, img.S3Key)
	put(t, fs, thumb)

	agent := &stubAgent{store: fs}
	h := &Handler{logger: zap.NewNop(), ds: newMemRows(img), agent: agent}
	rec := httptest.NewRecorder()
	h.DeleteFileAttachment(rec, deleteRequest(userID, img.ID))

	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, exists(t, fs, img.S3Key), "object should be deleted")
	require.False(t, exists(t, fs, thumb), "thumbnail should be deleted")
}

func TestDeleteFileAttachment_KeepsObjectSharedByReferenceCopy(t *testing.T) {
	fs := newLocalStore(t)
	userID := uuid.New()
	fileID := "file-shared"
	orig := models.FileAttachment{ID: uuid.New(), Name: "cat.png", FileType: "image/png", FileID: &fileID}
	orig.S3Key = storage.FileKeyForImage(userID, orig.ID, orig.Name)
	// A reference copy (datastore.CreateFileAttachmentReference) reuses s3_key and FileID.
	ref := models.FileAttachment{ID: uuid.New(), Name: orig.Name, FileType: orig.FileType, FileID: &fileID, S3Key: orig.S3Key}
	origThumb := storage.FileKeyForImageThumbnail(userID, orig.ID)
	put(t, fs, orig.S3Key)
	put(t, fs, origThumb)

	rows := newMemRows(orig, ref)
	agent := &stubAgent{store: fs}
	h := &Handler{logger: zap.NewNop(), ds: rows, agent: agent}

	// Deleting the original keeps the shared object and provider file for the reference copy.
	rec := httptest.NewRecorder()
	h.DeleteFileAttachment(rec, deleteRequest(userID, orig.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, exists(t, fs, orig.S3Key), "object is still referenced by the copy")
	require.False(t, exists(t, fs, origThumb), "the thumbnail is keyed by the deleted row's id, so it goes")
	require.Empty(t, agent.deletedFileID, "provider file is still referenced by the copy")

	// Deleting the last reference removes the object and the provider file.
	rec = httptest.NewRecorder()
	h.DeleteFileAttachment(rec, deleteRequest(userID, ref.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, exists(t, fs, orig.S3Key))
	require.Equal(t, fileID, agent.deletedFileID)
}

func TestDeleteFileAttachment_ObjectDeleteFailureDoesNotFailDelete(t *testing.T) {
	userID := uuid.New()
	doc := models.FileAttachment{ID: uuid.New(), Name: "a.txt", FileType: "text/plain", S3Key: "users/x/chats/y/a.txt"}
	fs := &stubFileStore{contentByKey: map[string][]byte{doc.S3Key: []byte("a")}, deleteErr: errors.New("s3 down")}
	rows := newMemRows(doc)
	h := &Handler{logger: zap.NewNop(), ds: rows, agent: &stubAgent{store: fs}}

	rec := httptest.NewRecorder()
	h.DeleteFileAttachment(rec, deleteRequest(userID, doc.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rows.rows, "row is deleted even though the object delete failed")
	require.Equal(t, []string{doc.S3Key}, fs.deletedKeys, "documents have no thumbnail to delete")
}

// #253: a chat-scoped document row without s3_key lives at users/{u}/chats/{chatID}/…; the
// fallback must use the chat id, not the chat message id.
func TestGetFileAttachmentContent_LegacyChatDocWithoutS3Key(t *testing.T) {
	fs := newLocalStore(t)
	userID := uuid.New()
	chatID := uuid.New()
	messageID := uuid.New()
	doc := models.FileAttachment{
		ID:            uuid.New(),
		Name:          "notes.txt",
		FileType:      "text/plain",
		ChatMessageID: &messageID,
		ChatID:        &chatID,
	}
	key := storage.FileKeyForChat(userID, chatID, doc.ID, doc.Name)
	require.NoError(t, fs.UploadFile(context.Background(), key, []byte("legacy notes"), "text/plain"))

	h := &Handler{logger: zap.NewNop(), ds: newMemRows(doc), agent: &stubAgent{store: fs}}
	req := httptest.NewRequest(http.MethodGet, "/file-attachment/"+doc.ID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"id": doc.ID.String()})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "legacy notes", rec.Body.String())
}
