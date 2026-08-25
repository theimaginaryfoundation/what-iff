package imagegallery

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
	"go.uber.org/zap"
)

// fakeGalleryStore serves a single fixed attachment for GetFileAttachment;
// the other Store methods are unused by GetImageContent.
type fakeGalleryStore struct {
	attachment *models.FileAttachment
}

func (s fakeGalleryStore) ListFileAttachments(context.Context, uuid.UUID, int, int, models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	return nil, nil
}

func (s fakeGalleryStore) GetFileAttachment(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
	return s.attachment, nil
}

func (s fakeGalleryStore) CreateFileAttachment(context.Context, uuid.UUID, models.FileAttachment) (*models.FileAttachment, error) {
	return nil, nil
}

func (s fakeGalleryStore) DeleteFileAttachment(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s fakeGalleryStore) UpdateFileAttachmentName(context.Context, uuid.UUID, uuid.UUID, string) (*models.FileAttachment, error) {
	return nil, nil
}

func (s fakeGalleryStore) CreateFileAttachmentReference(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
	return nil, nil
}

func (s fakeGalleryStore) SetFileAttachmentS3Key(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

// fakeGalleryFileStore returns fixed bytes for any key.
type fakeGalleryFileStore struct {
	content []byte
}

func (s fakeGalleryFileStore) UploadFile(context.Context, string, []byte, string) error { return nil }

func (s fakeGalleryFileStore) DownloadFile(context.Context, string) ([]byte, error) {
	return s.content, nil
}

func (s fakeGalleryFileStore) DeleteFile(context.Context, string) error { return nil }

func TestDedupeGalleryImages_ByS3Key(t *testing.T) {
	t.Parallel()

	a := &models.FileAttachment{ID: uuid.New(), S3Key: "images/user/a.png", Name: "a.png"}
	b := &models.FileAttachment{ID: uuid.New(), S3Key: "images/user/a.png", Name: "a copy.png"}
	c := &models.FileAttachment{ID: uuid.New(), S3Key: "images/user/c.png", Name: "c.png"}

	rows := []any{a, b, c}
	deduped := dedupeGalleryImages(rows)

	require.Len(t, deduped, 2)
	require.Same(t, a, deduped[0])
	require.Same(t, c, deduped[1])
}

func TestDedupeGalleryImages_FallbackToFileID(t *testing.T) {
	t.Parallel()

	fileID := "file-123"
	otherFileID := "file-999"
	a := &models.FileAttachment{ID: uuid.New(), FileID: &fileID, Name: "a.png"}
	b := &models.FileAttachment{ID: uuid.New(), FileID: &fileID, Name: "a ref.png"}
	c := &models.FileAttachment{ID: uuid.New(), FileID: &otherFileID, Name: "c.png"}

	rows := []any{a, b, c}
	deduped := dedupeGalleryImages(rows)

	require.Len(t, deduped, 2)
	require.Same(t, a, deduped[0])
	require.Same(t, c, deduped[1])
}

func TestDedupeGalleryImages_KeepsRowsWithoutIdentityKey(t *testing.T) {
	t.Parallel()

	a := &models.FileAttachment{ID: uuid.New(), Name: "no-key-1.png"}
	b := &models.FileAttachment{ID: uuid.New(), Name: "no-key-2.png"}
	c := "not-an-attachment"

	rows := []any{a, b, c}
	deduped := dedupeGalleryImages(rows)

	require.Len(t, deduped, 3)
	require.Same(t, a, deduped[0])
	require.Same(t, b, deduped[1])
	require.Equal(t, c, deduped[2])
}

func TestGetImageContent_SetsNosniffHeader(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	attachment := &models.FileAttachment{
		ID:       uuid.New(),
		UserID:   userID,
		Name:     "a.png",
		FileType: "image/png",
		S3Key:    "images/user/a.png",
	}
	store := fakeGalleryStore{attachment: attachment}
	fileStore := fakeGalleryFileStore{content: []byte("fake-png-bytes")}
	handler := NewHandler(store, zap.NewNop(), fileStore)

	req := httptest.NewRequest(http.MethodGet, "/image-gallery/"+attachment.ID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": attachment.ID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	handler.GetImageContent(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}
