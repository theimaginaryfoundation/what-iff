package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// importStore drives the async import handler: it captures the conversations forwarded to ImportChats
// and signals completion when the background job reaches a terminal status.
type importStore struct {
	fakeStore
	importFn func(ctx context.Context, userID uuid.UUID, convs []models.ImportConversation) (*models.ImportResult, error)

	mu            sync.Mutex
	capturedConvs []models.ImportConversation
	finalStatus   models.JobStatus
	done          chan struct{}
}

func newImportStore(fn func(ctx context.Context, userID uuid.UUID, convs []models.ImportConversation) (*models.ImportResult, error)) *importStore {
	return &importStore{importFn: fn, done: make(chan struct{})}
}

func (s *importStore) ImportChats(ctx context.Context, userID uuid.UUID, convs []models.ImportConversation, onProgress func(imported, skipped int)) (*models.ImportResult, error) {
	s.mu.Lock()
	s.capturedConvs = convs
	s.mu.Unlock()
	res, err := s.importFn(ctx, userID, convs)
	if onProgress != nil && res != nil {
		onProgress(res.Imported, res.Skipped)
	}
	return res, err
}

func (s *importStore) UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error) {
	if status == models.JobStatusComplete || status == models.JobStatusFailed {
		s.mu.Lock()
		s.finalStatus = status
		s.mu.Unlock()
		close(s.done)
	}
	return &models.Job{ID: id, Status: status, Error: errorMsg}, nil
}

// waitDone blocks until the background import reaches a terminal status, failing on timeout.
func (s *importStore) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("import background job did not finish in time")
	}
}

func (s *importStore) convs() []models.ImportConversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capturedConvs
}

func (s *importStore) status() models.JobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finalStatus
}

func importRequestNamed(t *testing.T, userID uuid.UUID, filename string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(body)
	require.NoError(t, err)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/chat/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if userID != uuid.Nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	}
	return req
}

func importRequest(t *testing.T, userID uuid.UUID, body []byte) *http.Request {
	return importRequestNamed(t, userID, "conversations.json", body)
}

func setupImportRouter(store Store) *mux.Router {
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	router.HandleFunc("/chat/import", h.ImportChats).Methods(http.MethodPost)
	return router
}

func noopImport(_ context.Context, _ uuid.UUID, convs []models.ImportConversation) (*models.ImportResult, error) {
	return &models.ImportResult{Imported: len(convs), Errors: []string{}}, nil
}

func TestImportChats_Unauthorized(t *testing.T) {
	t.Parallel()
	store := newImportStore(func(_ context.Context, _ uuid.UUID, _ []models.ImportConversation) (*models.ImportResult, error) {
		t.Fatal("should not reach datastore")
		return nil, nil
	})
	router := setupImportRouter(store)

	req := importRequest(t, uuid.Nil, []byte(`[]`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestImportChats_MissingFile(t *testing.T) {
	t.Parallel()
	router := setupImportRouter(newImportStore(func(_ context.Context, _ uuid.UUID, _ []models.ImportConversation) (*models.ImportResult, error) {
		t.Fatal("should not reach datastore")
		return nil, nil
	}))

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/chat/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestImportChats_UnrecognizedFormat(t *testing.T) {
	t.Parallel()
	router := setupImportRouter(newImportStore(func(_ context.Context, _ uuid.UUID, _ []models.ImportConversation) (*models.ImportResult, error) {
		t.Fatal("should not reach datastore")
		return nil, nil
	}))

	req := importRequest(t, uuid.New(), []byte(`not valid json`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestImportChats_OpenAIHappyPath(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := newImportStore(func(_ context.Context, uid uuid.UUID, convs []models.ImportConversation) (*models.ImportResult, error) {
		require.Equal(t, userID, uid)
		return &models.ImportResult{Imported: len(convs), Errors: []string{}}, nil
	})
	router := setupImportRouter(store)

	payload := []byte(`[
		{
			"id": "conv1",
			"title": "First Chat",
			"create_time": 1700000000,
			"current_node": "msg2",
			"mapping": {
				"msg1": {"id":"msg1","message":{"author":{"role":"user"},"create_time":1700000001,"content":{"content_type":"text","parts":["Hello"]}},"parent":null,"children":["msg2"]},
				"msg2": {"id":"msg2","message":{"author":{"role":"assistant"},"create_time":1700000002,"content":{"content_type":"text","parts":["Hi there"]}},"parent":"msg1","children":[]}
			}
		},
		{
			"id": "conv2",
			"title": "Second Chat",
			"create_time": 1700001000,
			"current_node": "m2",
			"mapping": {
				"m1": {"id":"m1","message":{"author":{"role":"user"},"create_time":1700001001,"content":{"content_type":"text","parts":["Question"]}},"parent":null,"children":["m2"]},
				"m2": {"id":"m2","message":{"author":{"role":"assistant"},"create_time":1700001002,"content":{"content_type":"text","parts":["Answer"]}},"parent":"m1","children":[]}
			}
		}
	]`)

	req := importRequest(t, userID, payload)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var job models.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&job))
	require.NotEqual(t, uuid.Nil, job.ID)

	store.waitDone(t)
	require.Equal(t, models.JobStatusComplete, store.status())
	convs := store.convs()
	require.Len(t, convs, 2)
	for _, c := range convs {
		require.Equal(t, models.ChatSourceOpenAI, c.Source)
	}
}

func TestImportChats_AnthropicHappyPath(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := newImportStore(noopImport)
	router := setupImportRouter(store)

	payload := []byte(`[
		{
			"uuid": "11111111-1111-4111-8111-111111111111",
			"name": "Claude Chat",
			"created_at": "2025-08-03T03:00:06.253385Z",
			"chat_messages": [
				{"uuid":"a","sender":"human","text":"Hello","created_at":"2025-08-03T03:00:06.97Z","content":[{"type":"text","text":"Hello"}]},
				{"uuid":"b","sender":"assistant","text":"Hi! How can I help?","created_at":"2025-08-03T03:00:10.09Z","content":[{"type":"text","text":"Hi! How can I help?"}]}
			]
		}
	]`)

	req := importRequest(t, userID, payload)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	store.waitDone(t)
	require.Equal(t, models.JobStatusComplete, store.status())
	convs := store.convs()
	require.Len(t, convs, 1)
	require.Equal(t, models.ChatSourceAnthropic, convs[0].Source)
	require.Len(t, convs[0].Messages, 2)
	require.Equal(t, models.MessageOriginUser, convs[0].Messages[0].Origin)
	require.Equal(t, models.MessageOriginAssistant, convs[0].Messages[1].Origin)
}

func TestMaxChatImportBytes_IsSixtyMegabyteClass(t *testing.T) {
	t.Parallel()
	require.Equal(t, 63<<20, maxChatImportBytes)
}

func TestImportChats_DatastoreErrorMarksJobFailed(t *testing.T) {
	t.Parallel()

	store := newImportStore(func(_ context.Context, _ uuid.UUID, _ []models.ImportConversation) (*models.ImportResult, error) {
		return nil, errors.New("db down")
	})
	router := setupImportRouter(store)

	payload := []byte(`[{
		"id": "c1",
		"title": "Chat",
		"create_time": 1700000000,
		"current_node": "m1",
		"mapping": {
			"m1": {"id":"m1","message":{"author":{"role":"user"},"create_time":1700000001,"content":{"content_type":"text","parts":["Hi"]}},"parent":null,"children":[]}
		}
	}]`)

	req := importRequest(t, uuid.New(), payload)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	store.waitDone(t)
	require.Equal(t, models.JobStatusFailed, store.status())
}
