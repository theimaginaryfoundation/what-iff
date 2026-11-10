package fileattachment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filechange"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func putContent(t *testing.T, h *Handler, id string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := requestWithUser(t, http.MethodPut, "/file-attachment/"+id+"/content")
	req.Body = http.NoBody
	if body != nil {
		req.Body = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body)).Body
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)
	return rr
}

func contentHandler(row *models.FileAttachment, swapOK bool) (*Handler, *stubFileStore) {
	files := &stubFileStore{contentByKey: map[string][]byte{}}
	store := stubStore{
		getFn: func(_ context.Context, userID, id uuid.UUID) (*models.FileAttachment, error) {
			if row == nil || id != row.ID {
				return nil, datastore.ErrFileAttachmentNotFound
			}
			cp := *row
			cp.UserID = userID
			return &cp, nil
		},
		swapFn: func(_ context.Context, _, _ uuid.UUID, _, newKey string, opts datastore.SwapFileObjectOptions) (bool, error) {
			if !swapOK || (opts.ExpectVersion != nil && *opts.ExpectVersion != row.ContentVersion) {
				return false, nil
			}
			row.S3Key, row.ContentVersion, row.ContentSHA256 = newKey, row.ContentVersion+1, opts.SHA256
			return true, nil
		},
	}
	return &Handler{ds: store, logger: zap.NewNop(), agent: &stubAgent{store: files}}, files
}

func TestReplaceContent_SavesAndReturnsTheNewVersion(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", FileType: "text/markdown", S3Key: "old", ContentVersion: 2}
	h, _ := contentHandler(row, true)
	rr := putContent(t, h, row.ID.String(), []byte("# hi"), map[string]string{"If-Match": `"2"`, "Content-Type": "text/markdown; charset=utf-8"})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, `"3"`, rr.Header().Get("ETag"))
	var got models.FileAttachment
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, 3, got.ContentVersion)
	require.NotEqual(t, "old", got.S3Key)
}

func TestReplaceContent_StaleVersionIs412WithTheCurrentOne(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", FileType: "text/markdown", S3Key: "old", ContentVersion: 5}
	h, files := contentHandler(row, true)
	rr := putContent(t, h, row.ID.String(), []byte("x"), map[string]string{"If-Match": "4"})
	require.Equal(t, http.StatusPreconditionFailed, rr.Code)
	var body struct {
		Details map[string]int `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, 5, body.Details["content_version"])
	require.Len(t, files.deletedKeys, 1, "the unused upload is dropped")
}

func TestReplaceContent_Refusals(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", FileType: "text/markdown", S3Key: "old", ContentVersion: 1}
	h, _ := contentHandler(row, true)
	require.Equal(t, http.StatusBadRequest, putContent(t, h, row.ID.String(), []byte("x"), map[string]string{"If-Match": "abc"}).Code)
	require.Equal(t, http.StatusBadRequest, putContent(t, h, "not-a-uuid", []byte("x"), nil).Code)
	require.Equal(t, http.StatusUnsupportedMediaType, putContent(t, h, row.ID.String(), []byte("x"), map[string]string{"Content-Type": "application/json"}).Code)
	require.Equal(t, http.StatusNotFound, putContent(t, h, uuid.New().String(), []byte("x"), nil).Code)
	skill := &models.FileAttachment{ID: uuid.New(), Name: "SKILL.md", FileType: "text/markdown", S3Key: "s", Origin: models.FileOriginSkill}
	hs, _ := contentHandler(skill, true)
	require.Equal(t, http.StatusForbidden, putContent(t, hs, skill.ID.String(), []byte("x"), nil).Code)
	noStore := &Handler{ds: stubStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) { return row, nil }}, logger: zap.NewNop(), agent: &stubAgent{}}
	require.Equal(t, http.StatusServiceUnavailable, putContent(t, noStore, row.ID.String(), []byte("x"), nil).Code)
	rr := putContent(t, h, row.ID.String(), []byte("x"), map[string]string{"If-Match": "1"})
	require.Equal(t, http.StatusOK, rr.Code, "a bare If-Match works too")
}

func TestReplaceContent_Unauthorized(t *testing.T) {
	h, _ := contentHandler(nil, true)
	req := httptest.NewRequest(http.MethodPut, "/file-attachment/"+uuid.New().String()+"/content", bytes.NewReader([]byte("x")))
	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestReplaceContent_ChatIDMustBeTheCallersAndSandboxedChatsAreNotNamed(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", FileType: "text/markdown", S3Key: "old", ContentVersion: 1}
	var events []filechange.Event
	filechange.Reset()
	t.Cleanup(filechange.Reset)
	filechange.Subscribe(func(_ context.Context, ev filechange.Event) { events = append(events, ev) })

	put := func(chat *models.Chat, err error) int {
		h, _ := contentHandler(row, true)
		store := h.ds.(stubStore)
		store.chatFn = func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) { return chat, err }
		h.ds = store
		req, _ := requestWithUser(t, http.MethodPut, "/file-attachment/"+row.ID.String()+"/content?chat_id="+uuid.NewString())
		req.Body = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader([]byte("x"))).Body
		rr := httptest.NewRecorder()
		r := mux.NewRouter()
		h.RegisterRoutes(r)
		r.ServeHTTP(rr, req)
		return rr.Code
	}

	require.Equal(t, http.StatusNotFound, put(nil, datastore.ErrChatNotFound))
	require.Empty(t, events, "nothing is saved for someone else's chat")
	require.Equal(t, http.StatusInternalServerError, put(nil, errors.New("db down")))

	require.Equal(t, http.StatusOK, put(&models.Chat{}, nil))
	require.NotNil(t, events[0].ChatID)

	require.Equal(t, http.StatusOK, put(&models.Chat{ContextScope: models.ContextScopeSandbox}, nil))
	require.Nil(t, events[1].ChatID)
}

func TestReplaceContent_AgentFileCap(t *testing.T) {
	row := &models.FileAttachment{ID: uuid.New(), Name: "a.md", FileType: "text/markdown", S3Key: "old", ContentVersion: 1, Origin: models.FileOriginAgent}
	h, _ := contentHandler(row, true)
	require.Equal(t, http.StatusRequestEntityTooLarge, putContent(t, h, row.ID.String(), make([]byte, models.AgentFileMaxBytes+1), nil).Code)
	require.Equal(t, http.StatusOK, putContent(t, h, row.ID.String(), make([]byte, models.AgentFileMaxBytes), nil).Code)
}
