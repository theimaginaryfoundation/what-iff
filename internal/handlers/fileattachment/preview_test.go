package fileattachment

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

func previewRequest(t *testing.T, h *Handler, method, url string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := requestWithUser(t, method, url)
	if body != nil {
		req.Body = httptest.NewRequest(method, "/", bytes.NewReader(body)).Body
	}
	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)
	return rr
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	return buf.Bytes()
}

func diagramRow() *models.FileAttachment {
	return &models.FileAttachment{ID: uuid.New(), Name: "flow.drawio", FileType: "application/vnd.jgraph.mxfile", S3Key: "users/u/files/flow.drawio", ContentVersion: 3}
}

func TestPutFilePreview_StoresThePictureAndAThumbnailBesideTheObject(t *testing.T) {
	row := diagramRow()
	h, files := contentHandler(row, true)
	pic := pngOf(t, 40, 20)
	rr := previewRequest(t, h, http.MethodPut, "/file-attachment/"+row.ID.String()+"/preview?version=3", pic)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	require.Equal(t, pic, files.uploaded[storage.FileKeyForPreview(row.S3Key)])
	require.NotEmpty(t, files.uploaded[storage.FileKeyForPreviewThumbnail(row.S3Key)])

	// Read back: the picture, then its thumbnail.
	files.contentByKey = files.uploaded
	rr = previewRequest(t, h, http.MethodGet, "/file-attachment/"+row.ID.String()+"/preview", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "image/png", rr.Header().Get("Content-Type"))
	require.Equal(t, pic, rr.Body.Bytes())
	rr = previewRequest(t, h, http.MethodGet, "/file-attachment/"+row.ID.String()+"/preview?size=thumbnail", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "image/jpeg", rr.Header().Get("Content-Type"))
}

func TestPutFilePreview_AWebPageTakesOne(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "index.html", FileType: "text/html", S3Key: "users/u/files/index.html", ContentVersion: 2}
	h, files := contentHandler(row, true)
	pic := pngOf(t, 64, 40)
	rr := previewRequest(t, h, http.MethodPut, "/file-attachment/"+row.ID.String()+"/preview?version=2", pic)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	require.Equal(t, pic, files.uploaded[storage.FileKeyForPreview(row.S3Key)])
}

// TestFilePreview_APagesPictureIsNotCurrentOnceAFileItLoadedChanged: a page's picture keeps the
// files it was drawn from (ADR 0x050 §8), and asking for a current one says there is none once one
// of them has a new version or is gone, as when an agent changed the page's stylesheet.
func TestFilePreview_APagesPictureIsNotCurrentOnceAFileItLoadedChanged(t *testing.T) {
	page := &models.FileAttachment{ID: uuid.New(), Name: "index.html", FileType: "text/html", S3Key: "users/u/files/index.html", ContentVersion: 2}
	css := &models.FileAttachment{ID: uuid.New(), Name: "app.css", Folder: "site/css", ContentVersion: 4}
	js := &models.FileAttachment{ID: uuid.New(), Name: "app.js", Folder: "site", ContentVersion: 1}
	rows := map[uuid.UUID]*models.FileAttachment{page.ID: page, css.ID: css, js.ID: js}
	files := &stubFileStore{contentByKey: map[string][]byte{}}
	h := &Handler{ds: stubStore{getFn: func(_ context.Context, _, id uuid.UUID) (*models.FileAttachment, error) {
		if r, ok := rows[id]; ok {
			cp := *r
			return &cp, nil
		}
		return nil, datastore.ErrFileAttachmentNotFound
	}}, logger: zap.NewNop(), agent: &stubAgent{store: files}}
	url := "/file-attachment/" + page.ID.String() + "/preview"

	rr := previewRequest(t, h, http.MethodPut, url+"?version=2&sources="+css.ID.String()+":4,"+js.ID.String()+":1", pngOf(t, 8, 8))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	files.contentByKey = files.uploaded

	require.Equal(t, http.StatusOK, previewRequest(t, h, http.MethodGet, url+"?size=thumbnail&current=1", nil).Code)
	css.ContentVersion = 5
	require.Equal(t, http.StatusNotFound, previewRequest(t, h, http.MethodGet, url+"?size=thumbnail&current=1", nil).Code)
	// The gallery still shows the picture it has.
	require.Equal(t, http.StatusOK, previewRequest(t, h, http.MethodGet, url+"?size=thumbnail", nil).Code)
	css.ContentVersion = 4
	delete(rows, js.ID)
	require.Equal(t, http.StatusNotFound, previewRequest(t, h, http.MethodGet, url+"?current=1", nil).Code)

	// A new picture without sources replaces them.
	rr = previewRequest(t, h, http.MethodPut, url+"?version=2", pngOf(t, 8, 8))
	require.Equal(t, http.StatusNoContent, rr.Code)
	files.contentByKey = files.uploaded
	require.Equal(t, http.StatusOK, previewRequest(t, h, http.MethodGet, url+"?current=1", nil).Code)

	rr = previewRequest(t, h, http.MethodPut, url+"?version=2&sources=nope", pngOf(t, 8, 8))
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPutFilePreview_RefusesAnOldVersionNonDiagramsAndNonPNGs(t *testing.T) {
	row := diagramRow()
	h, files := contentHandler(row, true)
	rr := previewRequest(t, h, http.MethodPut, "/file-attachment/"+row.ID.String()+"/preview?version=2", pngOf(t, 4, 4))
	require.Equal(t, http.StatusConflict, rr.Code)
	var body struct {
		Details map[string]int `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, 3, body.Details["content_version"])

	rr = previewRequest(t, h, http.MethodPut, "/file-attachment/"+row.ID.String()+"/preview?version=3", []byte("not a png"))
	require.Equal(t, http.StatusBadRequest, rr.Code)

	text := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", FileType: "text/markdown", S3Key: "k", ContentVersion: 1}
	h2, _ := contentHandler(text, true)
	rr = previewRequest(t, h2, http.MethodPut, "/file-attachment/"+text.ID.String()+"/preview?version=1", pngOf(t, 4, 4))
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Empty(t, files.uploaded)
}

func TestGetFilePreview_NoneOfTheCurrentVersionIs404(t *testing.T) {
	row := diagramRow()
	h, files := contentHandler(row, true)
	// A preview of the object before the last save is not this version's.
	files.contentByKey[storage.FileKeyForPreview("users/u/files/old.drawio")] = pngOf(t, 4, 4)
	rr := previewRequest(t, h, http.MethodGet, "/file-attachment/"+row.ID.String()+"/preview", nil)
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestFileKeyForPreview_IsUnderTheObjectsUser(t *testing.T) {
	require.Regexp(t, `^users/u1/previews/[0-9a-f]{32}\.png$`, storage.FileKeyForPreview("users/u1/chats/c/x.drawio"))
	require.NotEqual(t, storage.FileKeyForPreview("users/u1/a"), storage.FileKeyForPreview("users/u1/b"))
}

func TestPutFilePreview_RefusesAPictureTooLargeToDecode(t *testing.T) {
	row := diagramRow()
	h, files := contentHandler(row, true)
	for _, size := range [][2]int{{maxPreviewSide + 1, 1}, {1, maxPreviewSide + 1}, {3000, 3000}} {
		rr := previewRequest(t, h, http.MethodPut, "/file-attachment/"+row.ID.String()+"/preview?version=3", pngOf(t, size[0], size[1]))
		require.Equal(t, http.StatusBadRequest, rr.Code, "%v", size)
	}
	require.Empty(t, files.uploaded)
}

func TestGetFilePreview_ETagFollowsThePictureNotTheVersion(t *testing.T) {
	row := diagramRow()
	h, files := contentHandler(row, true)
	url := "/file-attachment/" + row.ID.String() + "/preview"
	put := func(pic []byte) {
		rr := previewRequest(t, h, http.MethodPut, url+"?version=3", pic)
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		files.contentByKey = files.uploaded
	}
	get := func(etag string) *httptest.ResponseRecorder {
		req, _ := requestWithUser(t, http.MethodGet, url)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		rr := httptest.NewRecorder()
		r := mux.NewRouter()
		h.RegisterRoutes(r)
		r.ServeHTTP(rr, req)
		return rr
	}

	put(pngOf(t, 40, 20))
	first := get("")
	require.Equal(t, http.StatusOK, first.Code)
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)

	// A current copy is answered from the tag alone: the picture is not downloaded.
	files.seenKeys = nil
	require.Equal(t, http.StatusNotModified, get(etag).Code)
	require.NotContains(t, files.seenKeys, storage.FileKeyForPreview(row.S3Key))

	// Replaced at the same version (an agent's render): the old copy is no longer current.
	put(pngOf(t, 30, 30))
	again := get(etag)
	require.Equal(t, http.StatusOK, again.Code)
	require.NotEqual(t, etag, again.Header().Get("ETag"))
	require.Equal(t, http.StatusNotModified, get(again.Header().Get("ETag")).Code)

	// A preview stored without a tag still gets one, from its bytes.
	for k := range files.contentByKey {
		if strings.HasSuffix(k, ".tag") {
			delete(files.contentByKey, k)
		}
	}
	legacy := get("")
	require.Equal(t, http.StatusOK, legacy.Code)
	require.Equal(t, http.StatusNotModified, get(legacy.Header().Get("ETag")).Code)
}
