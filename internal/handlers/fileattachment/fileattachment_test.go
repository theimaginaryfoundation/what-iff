package fileattachment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

type stubStore struct {
	chatFn           func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error)
	listFn           func(context.Context, uuid.UUID, int, int, models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	getFn            func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error)
	deleteFn         func(context.Context, uuid.UUID, uuid.UUID) error
	referencedKeysFn func(context.Context, []string) (map[string]bool, error)
	swapFn           func(context.Context, uuid.UUID, uuid.UUID, string, string, datastore.SwapFileObjectOptions) (bool, error)
	// comments is a file's stored comment set, when a test keeps one.
	comments *models.FileCommentSet
}

func (s stubStore) GetFileCommentSet(context.Context, uuid.UUID, uuid.UUID) (*models.FileCommentSet, error) {
	if s.comments == nil || s.comments.Revision == 0 {
		return nil, nil
	}
	cp := *s.comments
	return &cp, nil
}

func (s stubStore) PutFileCommentSet(_ context.Context, _, _ uuid.UUID, set models.FileCommentSet, expect int) (int, error) {
	if s.comments == nil {
		return 1, nil
	}
	if s.comments.Revision != expect {
		return 0, datastore.ErrFileCommentsRevision
	}
	set.Revision = expect + 1
	*s.comments = set
	return set.Revision, nil
}

func (s stubStore) GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error) {
	if s.chatFn != nil {
		return s.chatFn(ctx, userID, id)
	}
	return &models.Chat{}, nil
}

func (s stubStore) ReferencedFileAttachmentKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	if s.referencedKeysFn != nil {
		return s.referencedKeysFn(ctx, keys)
	}
	return map[string]bool{}, nil
}

func (s stubStore) ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	return s.listFn(ctx, userID, pageNum, pageSize, filters)
}

func (s stubStore) GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error) {
	return s.getFn(ctx, userID, id)
}

func (s stubStore) DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error {
	return s.deleteFn(ctx, userID, id)
}

func (s stubStore) ExistingFileAttachmentIDs(context.Context, []uuid.UUID) (map[uuid.UUID]bool, error) {
	return map[uuid.UUID]bool{}, nil
}

func (s stubStore) SwapFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string, opts datastore.SwapFileObjectOptions) (bool, error) {
	if s.swapFn != nil {
		return s.swapFn(ctx, userID, id, oldKey, newKey, opts)
	}
	return false, errors.New("swap not stubbed")
}

func (s stubStore) ClearFileChunks(context.Context, uuid.UUID) error { return nil }

type stubAgent struct {
	store storage.FileStore
}

func (a *stubAgent) FileStore() storage.FileStore                  { return a.store }
func (a *stubAgent) ChunkPipeline() *filechunker.FileChunkPipeline { return nil }

type stubFileStore struct {
	contentByKey map[string][]byte
	errByKey     map[string]error
	seenKeys     []string
	deletedKeys  []string
	deleteErr    error
}

func (s *stubFileStore) UploadFile(context.Context, string, []byte, string) error { return nil }
func (s *stubFileStore) DeleteFile(_ context.Context, key string) error {
	s.deletedKeys = append(s.deletedKeys, key)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.contentByKey, key)
	return nil
}
func (s *stubFileStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	s.seenKeys = append(s.seenKeys, key)
	if err := s.errByKey[key]; err != nil {
		return nil, err
	}
	return s.contentByKey[key], nil
}

func requestWithUser(t *testing.T, method, url string) (*http.Request, uuid.UUID) {
	t.Helper()
	userID := uuid.New()
	req := httptest.NewRequest(method, url, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	return req, userID
}

func TestListFileAttachments_Unauthorized(t *testing.T) {
	h := &Handler{logger: zap.NewNop()}
	req := httptest.NewRequest(http.MethodGet, "/file-attachment", nil)
	rec := httptest.NewRecorder()

	h.ListFileAttachments(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListFileAttachments_ParsesFiltersAndReturnsPage(t *testing.T) {
	var gotPage, gotLimit int
	var gotFilters models.FileAttachmentFilters
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			listFn: func(_ context.Context, _ uuid.UUID, page, limit int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
				gotPage, gotLimit = page, limit
				gotFilters = filters
				return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: page}, nil
			},
		},
	}
	msgID := uuid.New()
	persID := uuid.New()
	minDate := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	maxDate := time.Now().UTC().Format(time.RFC3339)
	req, _ := requestWithUser(t, http.MethodGet,
		"/file-attachment?page=2&limit=7&name=notes&file_type=text&chat_message_id="+msgID.String()+"&personality_id="+persID.String()+"&docs_only=true&min_date="+minDate+"&max_date="+maxDate)
	rec := httptest.NewRecorder()

	h.ListFileAttachments(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 2, gotPage)
	require.Equal(t, 7, gotLimit)
	require.NotNil(t, gotFilters.Name)
	require.Equal(t, "notes", *gotFilters.Name)
	require.NotNil(t, gotFilters.FileType)
	require.Equal(t, "text", *gotFilters.FileType)
	require.NotNil(t, gotFilters.ChatMessageID)
	require.Equal(t, msgID, *gotFilters.ChatMessageID)
	require.NotNil(t, gotFilters.PersonalityID)
	require.Equal(t, persID, *gotFilters.PersonalityID)
	require.NotNil(t, gotFilters.DocsOnly)
	require.True(t, *gotFilters.DocsOnly)
	require.NotNil(t, gotFilters.MinDate)
	require.NotNil(t, gotFilters.MaxDate)
}

func TestDeleteFileAttachment_InvalidID(t *testing.T) {
	h := &Handler{logger: zap.NewNop(), ds: stubStore{}, agent: &stubAgent{}}
	req, _ := requestWithUser(t, http.MethodDelete, "/file-attachment/not-a-uuid")
	req = mux.SetURLVars(req, map[string]string{"id": "not-a-uuid"})
	rec := httptest.NewRecorder()

	h.DeleteFileAttachment(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// A row left over from the OpenAI Files era still carries a file_id. Nothing is deleted remotely
// for it any more, so it deletes like any other row.
func TestDeleteFileAttachment_LegacyProviderFileIDIsIgnored(t *testing.T) {
	fileID := "provider-file-id"
	deleted := false
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return &models.FileAttachment{ID: uuid.New(), FileID: &fileID}, nil
			},
			deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
				deleted = true
				return nil
			},
		},
		agent: &stubAgent{},
	}
	id := uuid.New()
	req, _ := requestWithUser(t, http.MethodDelete, "/file-attachment/"+id.String())
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	rec := httptest.NewRecorder()

	h.DeleteFileAttachment(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, deleted)
}

func TestDeleteFileAttachment_Success(t *testing.T) {
	deleted := false
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return &models.FileAttachment{ID: uuid.New()}, nil
			},
			deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
				deleted = true
				return nil
			},
		},
		agent: &stubAgent{},
	}
	id := uuid.New()
	req, _ := requestWithUser(t, http.MethodDelete, "/file-attachment/"+id.String())
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	rec := httptest.NewRecorder()

	h.DeleteFileAttachment(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, deleted)
}

func TestGetFileAttachmentContent_NotFound(t *testing.T) {
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return nil, datastore.ErrFileAttachmentNotFound
			},
		},
		agent: &stubAgent{},
	}
	id := uuid.New()
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment/"+id.String())
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetFileAttachmentContent_FileStoreNotConfigured(t *testing.T) {
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return &models.FileAttachment{ID: uuid.New(), Name: "report.pdf", FileType: "application/pdf"}, nil
			},
		},
		agent: &stubAgent{},
	}
	id := uuid.New()
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment/"+id.String())
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetFileAttachmentContent_DownloadsFallbackKey(t *testing.T) {
	attachmentID := uuid.New()
	userID := uuid.New()
	chatID := uuid.New()
	messageID := uuid.New()
	attachment := &models.FileAttachment{
		ID:            attachmentID,
		Name:          "notes.txt",
		FileType:      "text/plain",
		ChatMessageID: &messageID,
		ChatID:        &chatID,
		S3Key:         "legacy/key",
	}
	expectedFallback := storage.FileKeyForChat(userID, chatID, attachmentID, attachment.Name)

	fs := &stubFileStore{
		contentByKey: map[string][]byte{
			"legacy/key":     nil,
			expectedFallback: []byte("hello world"),
		},
		errByKey: map[string]error{},
	}
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return attachment, nil
			},
		},
		agent: &stubAgent{store: fs},
	}

	req := httptest.NewRequest(http.MethodGet, "/file-attachment/"+attachmentID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"id": attachmentID.String()})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="notes.txt"`, rec.Header().Get("Content-Disposition"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, []string{"legacy/key", expectedFallback}, fs.seenKeys)
	require.Equal(t, "hello world", rec.Body.String())
}

func TestGetFileAttachmentContent_RevalidatesByContentVersion(t *testing.T) {
	attachmentID := uuid.New()
	userID := uuid.New()
	attachment := &models.FileAttachment{ID: attachmentID, Name: "notes.txt", FileType: "text/plain", S3Key: "k", ContentVersion: 3}
	fs := &stubFileStore{contentByKey: map[string][]byte{"k": []byte("v3")}, errByKey: map[string]error{}}
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return attachment, nil
			},
		},
		agent: &stubAgent{store: fs},
	}
	get := func(ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/file-attachment/"+attachmentID.String(), nil)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
		req = mux.SetURLVars(req, map[string]string{"id": attachmentID.String()})
		rec := httptest.NewRecorder()
		h.GetFileAttachmentContent(rec, req)
		return rec
	}

	first := get("")
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, `"3"`, first.Header().Get("ETag"))
	require.Equal(t, "private, no-cache", first.Header().Get("Cache-Control"))

	current := get(`"3"`)
	require.Equal(t, http.StatusNotModified, current.Code)
	require.Empty(t, current.Body.String())
	require.Len(t, fs.seenKeys, 1, "a current copy needs no read from the store")

	// A save moved the version on: the old copy gets the new bytes.
	stale := get(`"2"`)
	require.Equal(t, http.StatusOK, stale.Code)
	require.Equal(t, "v3", stale.Body.String())
}

func TestGetFileAttachmentContent_DownloadError(t *testing.T) {
	id := uuid.New()
	fs := &stubFileStore{
		contentByKey: map[string][]byte{},
		errByKey:     map[string]error{"primary": errors.New("download failed")},
	}
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return &models.FileAttachment{ID: id, Name: "x.txt", FileType: "text/plain", S3Key: "primary"}, nil
			},
		},
		agent: &stubAgent{store: fs},
	}
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment/"+id.String())
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetFileAttachmentContent_ContentNotAvailable(t *testing.T) {
	id := uuid.New()
	fs := &stubFileStore{contentByKey: map[string][]byte{}, errByKey: map[string]error{}}
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
				return &models.FileAttachment{ID: id, Name: "x.txt", FileType: ""}, nil
			},
		},
		agent: &stubAgent{store: fs},
	}
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment/"+id.String())
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRegisterRoutes_WiresEndpoints(t *testing.T) {
	h := &Handler{logger: zap.NewNop(), ds: stubStore{}, agent: &stubAgent{}}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/file-attachment", nil)
	match := &mux.RouteMatch{}
	require.True(t, router.Match(req, match))
}

func TestListFileAttachments_DatastoreErrorReturns500(t *testing.T) {
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			listFn: func(context.Context, uuid.UUID, int, int, models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
				return nil, errors.New("db down")
			},
		},
		agent: &stubAgent{},
	}
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment")
	rec := httptest.NewRecorder()

	h.ListFileAttachments(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetFileAttachmentContent_Unauthorized(t *testing.T) {
	h := &Handler{logger: zap.NewNop(), ds: stubStore{}, agent: &stubAgent{}}
	req := httptest.NewRequest(http.MethodGet, "/file-attachment/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetFileAttachmentContent_InvalidID(t *testing.T) {
	h := &Handler{logger: zap.NewNop(), ds: stubStore{}, agent: &stubAgent{}}
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment/not-a-uuid")
	req = mux.SetURLVars(req, map[string]string{"id": "not-a-uuid"})
	rec := httptest.NewRecorder()

	h.GetFileAttachmentContent(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteFileAttachment_Unauthorized(t *testing.T) {
	h := &Handler{logger: zap.NewNop(), ds: stubStore{}, agent: &stubAgent{}}
	req := httptest.NewRequest(http.MethodDelete, "/file-attachment/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()

	h.DeleteFileAttachment(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListFileAttachments_IgnoresInvalidDocsOnly(t *testing.T) {
	var captured models.FileAttachmentFilters
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			listFn: func(_ context.Context, _ uuid.UUID, _ int, _ int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
				captured = filters
				return &models.PaginatedResponse{Results: []any{}}, nil
			},
		},
		agent: &stubAgent{},
	}
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment?docs_only=nope")
	rec := httptest.NewRecorder()
	h.ListFileAttachments(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, captured.DocsOnly)
}

func TestListFileAttachments_ResponseIsJSON(t *testing.T) {
	h := &Handler{
		logger: zap.NewNop(),
		ds: stubStore{
			listFn: func(_ context.Context, _ uuid.UUID, _ int, _ int, _ models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
				return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
			},
		},
		agent: &stubAgent{},
	}
	req, _ := requestWithUser(t, http.MethodGet, "/file-attachment")
	rec := httptest.NewRecorder()
	h.ListFileAttachments(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
}
