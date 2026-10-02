package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// TestDeleteChat_ReleasesAttachmentObjects: the chat cascade removes the chat's attachment rows,
// so the handler deletes their objects afterwards, except one a reference copy in another chat
// still points at.
func TestDeleteChat_ReleasesAttachmentObjects(t *testing.T) {
	t.Setenv("LOCAL_FILE_STORE_DIR", t.TempDir())
	fs, err := storage.NewFileStore(context.Background(), "", "", zap.NewNop())
	require.NoError(t, err)

	userID, chatID, otherChatID := uuid.New(), uuid.New(), uuid.New()
	doc := models.FileAttachment{ID: uuid.New(), Name: "notes.txt", FileType: "text/plain", ChatID: &chatID}
	doc.S3Key = storage.FileKeyForChat(userID, chatID, doc.ID, doc.Name)
	legacyDoc := models.FileAttachment{ID: uuid.New(), Name: "old.txt", FileType: "text/plain", ChatID: &chatID}
	img := models.FileAttachment{ID: uuid.New(), Name: "cat.png", FileType: "image/png", ChatID: &chatID}
	img.S3Key = storage.FileKeyForImage(userID, img.ID, img.Name)
	sharedImg := models.FileAttachment{ID: uuid.New(), Name: "dog.png", FileType: "image/png", ChatID: &chatID}
	sharedImg.S3Key = storage.FileKeyForImage(userID, sharedImg.ID, sharedImg.Name)
	refCopy := models.FileAttachment{ID: uuid.New(), Name: "dog.png", FileType: "image/png", ChatID: &otherChatID, S3Key: sharedImg.S3Key}

	legacyKey := storage.FileKeyForChat(userID, chatID, legacyDoc.ID, legacyDoc.Name)
	keys := []string{doc.S3Key, legacyKey, img.S3Key, storage.FileKeyForImageThumbnail(userID, img.ID), sharedImg.S3Key}
	for _, k := range keys {
		require.NoError(t, fs.UploadFile(context.Background(), k, []byte(k), "application/octet-stream"))
	}

	rows := []models.FileAttachment{doc, legacyDoc, img, sharedImg, refCopy}
	store := &fakeStore{
		listChatAttachmentRefsFn: func(_ context.Context, _ uuid.UUID, id uuid.UUID) ([]models.FileAttachment, error) {
			var out []models.FileAttachment
			for _, r := range rows {
				if r.ChatID != nil && *r.ChatID == id {
					out = append(out, r)
				}
			}
			return out, nil
		},
		deleteChatFn: func(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
			kept := rows[:0]
			for _, r := range rows {
				if r.ChatID == nil || *r.ChatID != id {
					kept = append(kept, r)
				}
			}
			rows = kept
			return nil
		},
		existingIDsFn: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
			out := map[uuid.UUID]bool{}
			for _, id := range ids {
				for _, r := range rows {
					if r.ID == id {
						out[id] = true
					}
				}
			}
			return out, nil
		},
		referencedKeysFn: func(_ context.Context, ks []string) (map[string]bool, error) {
			out := map[string]bool{}
			for _, k := range ks {
				for _, r := range rows {
					if r.S3Key == k {
						out[k] = true
					}
				}
			}
			return out, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.files = fs

	req := httptest.NewRequest(http.MethodDelete, "/chat/"+chatID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	rec := httptest.NewRecorder()
	h.DeleteChat(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	has := func(k string) bool {
		b, err := fs.DownloadFile(context.Background(), k)
		require.NoError(t, err)
		return b != nil
	}
	require.False(t, has(doc.S3Key))
	require.False(t, has(legacyKey), "legacy rows without s3_key release their derived key")
	require.False(t, has(img.S3Key))
	require.False(t, has(storage.FileKeyForImageThumbnail(userID, img.ID)))
	require.True(t, has(sharedImg.S3Key), "the reference copy in another chat still uses it")
}
