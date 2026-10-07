package imagegallery

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestListAndFoldersTakeAKindThatDefaultsToImages(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		want  models.GalleryKind
	}{
		"absent is images, as before": {query: "", want: models.GalleryKindImages},
		"images":                      {query: "?kind=images", want: models.GalleryKindImages},
		"files":                       {query: "?kind=files", want: models.GalleryKindFiles},
		"all, any case":               {query: "?kind=ALL", want: models.GalleryKindAll},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeFolderStore{}
			rr := serve(t, store, http.MethodGet, "/image-gallery"+tc.query, "")
			require.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.want, store.gotFilters.Kind)
			assert.Nil(t, store.gotFilters.FileType, "kind replaces the old MIME substring filter")

			rr = serve(t, store, http.MethodGet, "/image-gallery/folders"+tc.query, "")
			require.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.want, store.folderKind)
		})
	}
}

func TestAnUnknownKindIsRefused(t *testing.T) {
	store := &fakeFolderStore{}
	assert.Equal(t, http.StatusBadRequest, serve(t, store, http.MethodGet, "/image-gallery?kind=videos", "").Code)
	assert.Equal(t, http.StatusBadRequest, serve(t, store, http.MethodGet, "/image-gallery/folders?kind=videos", "").Code)
	assert.Zero(t, store.listFilters, "nothing is queried")
}

// infoStore answers GetFileAttachment for one known file.
type infoStore struct {
	fakeFolderStore
	file *models.FileAttachment
}

func (s *infoStore) GetFileAttachment(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.FileAttachment, error) {
	if s.file != nil && s.file.ID == id {
		return s.file, nil
	}
	return nil, datastore.ErrFileAttachmentNotFound
}

func TestFileInfoReturnsAnyKindOfFile(t *testing.T) {
	doc := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", FileType: "text/markdown", Folder: "docs"}
	store := &infoStore{file: doc}

	rr := serve(t, store, http.MethodGet, "/image-gallery/"+doc.ID.String()+"/info", "")
	require.Equal(t, http.StatusOK, rr.Code)
	var got models.FileAttachment
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "notes.md", got.Name)
	assert.Equal(t, "text/markdown", got.FileType)
	assert.Equal(t, "docs", got.Folder)

	assert.Equal(t, http.StatusNotFound, serve(t, store, http.MethodGet, "/image-gallery/"+uuid.NewString()+"/info", "").Code)
	assert.Equal(t, http.StatusBadRequest, serve(t, store, http.MethodGet, "/image-gallery/not-a-uuid/info", "").Code)
}
