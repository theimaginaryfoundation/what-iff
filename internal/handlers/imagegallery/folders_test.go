package imagegallery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeFolderStore records what the folder handlers pass to the datastore.
type fakeFolderStore struct {
	Store
	folders     []models.FolderCount
	gotFilters  models.FileAttachmentFilters
	movedIDs    []uuid.UUID
	movedTo     string
	moveFrom    string
	moveTo      string
	moveCount   int
	moveErr     error
	listFilters int
	folderKind  models.GalleryKind
}

func (f *fakeFolderStore) ListFileAttachments(_ context.Context, _ uuid.UUID, _, _ int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	f.gotFilters = filters
	f.listFilters++
	return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
}

func (f *fakeFolderStore) ListGalleryFolders(_ context.Context, _ uuid.UUID, kind models.GalleryKind) ([]models.FolderCount, error) {
	f.folderKind = kind
	return f.folders, nil
}

func (f *fakeFolderStore) MoveFileAttachmentsToFolder(_ context.Context, _ uuid.UUID, ids []uuid.UUID, folder string) (int, error) {
	f.movedIDs, f.movedTo = ids, folder
	return len(ids), f.moveErr
}

func (f *fakeFolderStore) MoveGalleryFolder(_ context.Context, _ uuid.UUID, from, to string) (int, error) {
	f.moveFrom, f.moveTo = from, to
	return f.moveCount, f.moveErr
}

func serve(t *testing.T, store Store, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(store, zap.NewNop(), nil)
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestListImagesFolderFilterIsAPresenceCheck(t *testing.T) {
	for name, tc := range map[string]struct {
		target string
		want   *string
	}{
		"absent lists every image":      {target: "/image-gallery", want: nil},
		"empty is the top level":        {target: "/image-gallery?folder=", want: strp("")},
		"a folder is normalized":        {target: "/image-gallery?folder=Charts%2FOura%2F", want: strp("charts/oura")},
		"slashes only is the top level": {target: "/image-gallery?folder=%2F", want: strp("")},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeFolderStore{}
			rr := serve(t, store, http.MethodGet, tc.target, "")
			require.Equal(t, http.StatusOK, rr.Code)
			if tc.want == nil {
				assert.Nil(t, store.gotFilters.Folder)
			} else {
				require.NotNil(t, store.gotFilters.Folder)
				assert.Equal(t, *tc.want, *store.gotFilters.Folder)
			}
		})
	}
}

func TestListImagesRejectsABadFolder(t *testing.T) {
	store := &fakeFolderStore{}
	rr := serve(t, store, http.MethodGet, "/image-gallery?folder=..%2Fup", "")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Zero(t, store.listFilters, "nothing is queried")
}

func strp(s string) *string { return &s }

func TestListFoldersIsNotMistakenForAnImageID(t *testing.T) {
	store := &fakeFolderStore{folders: []models.FolderCount{{Path: "charts", Count: 2}}}

	rr := serve(t, store, http.MethodGet, "/image-gallery/folders", "")

	require.Equal(t, http.StatusOK, rr.Code, "the /folders route wins over /{id}")
	var got folderListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, []models.FolderCount{{Path: "charts", Count: 2}}, got.Folders)
}

func TestMoveImagesNormalizesTheFolderAndReportsHowManyMoved(t *testing.T) {
	store := &fakeFolderStore{}
	a, b := uuid.New(), uuid.New()
	body, _ := json.Marshal(map[string]any{"ids": []uuid.UUID{a, b}, "folder": " Daily Graphs / 2026 "})

	rr := serve(t, store, http.MethodPost, "/image-gallery/move", string(body))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "daily graphs/2026", store.movedTo)
	assert.Equal(t, []uuid.UUID{a, b}, store.movedIDs)
	assert.JSONEq(t, `{"moved":2}`, rr.Body.String())
}

func TestMoveImagesToTheTopLevel(t *testing.T) {
	store := &fakeFolderStore{}
	body, _ := json.Marshal(map[string]any{"ids": []uuid.UUID{uuid.New()}, "folder": ""})
	rr := serve(t, store, http.MethodPost, "/image-gallery/move", string(body))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "", store.movedTo)
}

func TestMoveImagesRefusesBadRequests(t *testing.T) {
	tooMany := make([]uuid.UUID, maxMoveIDs+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}
	tooManyBody, _ := json.Marshal(map[string]any{"ids": tooMany, "folder": "x"})
	for name, body := range map[string]string{
		"not json":        `nope`,
		"no ids":          `{"ids":[],"folder":"x"}`,
		"bad id":          `{"ids":["not-a-uuid"],"folder":"x"}`,
		"bad folder":      `{"ids":["` + uuid.NewString() + `"],"folder":"a/../b"}`,
		"too many ids":    string(tooManyBody),
		"folder too deep": `{"ids":["` + uuid.NewString() + `"],"folder":"` + strings.Repeat("a/", 9) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeFolderStore{}
			rr := serve(t, store, http.MethodPost, "/image-gallery/move", body)
			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Nil(t, store.movedIDs, "nothing is moved")
		})
	}
}

func TestMoveFolderNormalizesBothPaths(t *testing.T) {
	store := &fakeFolderStore{moveCount: 5}
	rr := serve(t, store, http.MethodPost, "/image-gallery/folders/move", `{"from":"Charts","to":"Archive/Charts"}`)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "charts", store.moveFrom)
	assert.Equal(t, "archive/charts", store.moveTo)
	assert.JSONEq(t, `{"moved":5}`, rr.Body.String())
}

func TestMoveFolderMapsRefusalsToBadRequest(t *testing.T) {
	store := &fakeFolderStore{moveErr: datastore.ErrFolderIntoItself}
	rr := serve(t, store, http.MethodPost, "/image-gallery/folders/move", `{"from":"a","to":"a/b"}`)
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	store = &fakeFolderStore{}
	rr = serve(t, store, http.MethodPost, "/image-gallery/folders/move", `{"from":"a","to":"b/../c"}`)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Empty(t, store.moveFrom, "an invalid path never reaches the datastore")

	store = &fakeFolderStore{moveErr: assert.AnError}
	rr = serve(t, store, http.MethodPost, "/image-gallery/folders/move", `{"from":"a","to":"b"}`)
	assert.Equal(t, http.StatusInternalServerError, rr.Code, "anything else is a server error")
}

func TestFolderRoutesNeedASignedInUser(t *testing.T) {
	h := NewHandler(&fakeFolderStore{}, zap.NewNop(), nil)
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/image-gallery/folders"},
		{http.MethodPost, "/image-gallery/move"},
		{http.MethodPost, "/image-gallery/folders/move"},
	} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.target, strings.NewReader("{}")))
		assert.Equal(t, http.StatusUnauthorized, rr.Code, tc.target)
	}
}
