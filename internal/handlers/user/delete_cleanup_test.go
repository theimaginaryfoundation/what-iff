package user

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// TestDeleteUser_PurgesUserObjects: account deletion removes everything under users/{id}/ and
// the account's export bundles under exports/{id}/, and leaves other users' objects alone.
func TestDeleteUser_PurgesUserObjects(t *testing.T) {
	t.Setenv("LOCAL_FILE_STORE_DIR", t.TempDir())
	fs, err := storage.NewFileStore(context.Background(), "", "", zap.NewNop())
	require.NoError(t, err)

	userID, otherID := uuid.New(), uuid.New()
	mine := []string{
		storage.FileKeyForImage(userID, uuid.New(), "a.png"),
		storage.FileKeyForImageThumbnail(userID, uuid.New()),
		storage.FileKeyForChat(userID, uuid.New(), uuid.New(), "b.txt"),
		storage.UserExportPrefix(userID) + "account-export-20260101-000000.zip",
	}
	theirs := storage.FileKeyForImage(otherID, uuid.New(), "c.png")
	for _, k := range append(mine, theirs) {
		require.NoError(t, fs.UploadFile(context.Background(), k, []byte(k), "application/octet-stream"))
	}

	store := &mockUserStore{}
	h := NewHandler(store, zap.NewNop(), nil, "dev").WithFileStore(fs)
	req := httptest.NewRequest(http.MethodDelete, "/user/delete", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()
	h.DeleteUser(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, userID, store.deletedUserID)
	for _, k := range mine {
		b, err := fs.DownloadFile(context.Background(), k)
		require.NoError(t, err)
		require.Nil(t, b, k)
	}
	b, err := fs.DownloadFile(context.Background(), theirs)
	require.NoError(t, err)
	require.NotNil(t, b, "other users' objects are untouched")
}
