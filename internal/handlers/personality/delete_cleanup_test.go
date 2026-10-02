package personality

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

// TestDeletePersonality_ReleasesAttachmentObjects: the personality cascade removes its
// attachment rows, so the handler deletes their objects afterwards.
func TestDeletePersonality_ReleasesAttachmentObjects(t *testing.T) {
	t.Setenv("LOCAL_FILE_STORE_DIR", t.TempDir())
	fs, err := storage.NewFileStore(context.Background(), "", "", zap.NewNop())
	require.NoError(t, err)

	userID, personalityID := uuid.New(), uuid.New()
	doc := models.FileAttachment{ID: uuid.New(), Name: "lore.md", FileType: "text/markdown", PersonalityID: &personalityID}
	doc.S3Key = storage.FileKeyForPersonality(userID, personalityID, doc.ID, doc.Name)
	require.NoError(t, fs.UploadFile(context.Background(), doc.S3Key, []byte("lore"), "text/markdown"))

	deleted := false
	store := &fakeStore{
		listPersonalityAttachmentRefsFn: func(context.Context, uuid.UUID, uuid.UUID) ([]models.FileAttachment, error) {
			return []models.FileAttachment{doc}, nil
		},
		deletePersonalityFn: func(context.Context, uuid.UUID, uuid.UUID) error {
			deleted = true
			return nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil)
	h.files = fs

	req := httptest.NewRequest(http.MethodDelete, "/personality/"+personalityID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"id": personalityID.String()})
	rec := httptest.NewRecorder()
	h.DeletePersonality(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, deleted)
	b, err := fs.DownloadFile(context.Background(), doc.S3Key)
	require.NoError(t, err)
	require.Nil(t, b, "object should be deleted with the personality")
}
