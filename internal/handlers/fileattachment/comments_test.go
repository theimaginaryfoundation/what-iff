package fileattachment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func commentsCall(t *testing.T, h *Handler, method, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := requestWithUser(t, method, "/file-attachment/"+id+"/comments")
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req.Body = httptest.NewRequest(method, "/", bytes.NewReader(raw)).Body
	}
	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)
	return rr
}

func TestFileComments_WriteThenReadAcrossAChange(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "notes.txt", FileType: "text/plain", S3Key: "v1", ContentVersion: 1}
	h, files := contentHandler(row, true)
	set := &models.FileCommentSet{}
	store := h.ds.(stubStore)
	store.comments = set
	h.ds = store
	files.contentByKey["v1"] = []byte("This is an inline comment in a text file")

	thread := models.FileCommentThread{ID: "t1", Anchor: &models.FileCommentRange{Start: 0, End: 40}, Comments: []models.FileComment{{ID: "t1", Text: "Hmm"}}}
	rr := commentsCall(t, h, http.MethodPut, row.ID.String(), putFileCommentsRequest{Threads: []models.FileCommentThread{thread}, ContentVersion: 1, Revision: 0})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// The file changes elsewhere: the next read carries the anchor.
	files.contentByKey["v2"] = []byte("This is an inline comment in Go source code")
	row.S3Key, row.ContentVersion = "v2", 2
	rr = commentsCall(t, h, http.MethodGet, row.ID.String(), nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got models.FileComments
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, 2, got.ContentVersion)
	require.Equal(t, "This is an inline comment in", got.Threads[0].Quote)

	// A write placed on the old content, or over a stale revision, is refused.
	rr = commentsCall(t, h, http.MethodPut, row.ID.String(), putFileCommentsRequest{Threads: got.Threads, ContentVersion: 1, Revision: got.Revision})
	require.Equal(t, http.StatusPreconditionFailed, rr.Code)
	rr = commentsCall(t, h, http.MethodPut, row.ID.String(), putFileCommentsRequest{Threads: got.Threads, ContentVersion: 2, Revision: 1})
	require.Equal(t, http.StatusConflict, rr.Code)
}

func TestFileComments_Refusals(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "img.bin", FileType: "application/octet-stream", S3Key: "k", ContentVersion: 1}
	h, files := contentHandler(row, true)
	files.contentByKey["k"] = []byte{0xff, 0xfe, 0x00}
	rr := commentsCall(t, h, http.MethodPut, row.ID.String(), putFileCommentsRequest{ContentVersion: 1})
	require.Equal(t, http.StatusUnsupportedMediaType, rr.Code)
	rr = commentsCall(t, h, http.MethodGet, uuid.NewString(), nil)
	require.Equal(t, http.StatusNotFound, rr.Code)
	rr = commentsCall(t, h, http.MethodPut, row.ID.String(), map[string]any{"content_version": 0})
	require.Equal(t, http.StatusBadRequest, rr.Code)
}
