package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// serveFileAttachment posts a multipart upload to the file-attachment route. A nil userID sends
// the request unauthenticated.
func serveFileAttachment(t *testing.T, chatID string, userID *uuid.UUID, fileName string) *httptest.ResponseRecorder {
	t.Helper()
	// An empty agent is enough for the cases that are rejected before the provider or store is used.
	return serveFileAttachmentWith(t, NewHandler(&fakeStore{}, zap.NewNop(), &agent.Agent{}, HandlerConfig{}), chatID, userID, fileName)
}

func serveFileAttachmentWith(t *testing.T, h *Handler, chatID string, userID *uuid.UUID, fileName string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("attachment", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("MZ")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID+"/file-attachment", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if userID != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, *userID))
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestCreateFileAttachment_Unauthorized(t *testing.T) {
	t.Parallel()

	w := serveFileAttachment(t, uuid.NewString(), nil, "notes.txt")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestCreateFileAttachment_InvalidChatID(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	w := serveFileAttachment(t, "not-a-uuid", &userID, "notes.txt")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid chat ID") {
		t.Fatalf("expected an invalid chat id message, got %q", w.Body.String())
	}
}

func TestCreateFileAttachment_RejectedUpload(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	w := serveFileAttachment(t, uuid.NewString(), &userID, "binary.exe")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "Unsupported file type") {
		t.Fatalf("expected the unsupported file type message, got %q", w.Body.String())
	}
}

// filesAPIStub answers the two OpenAI Files API calls an upload makes: create and (on rollback)
// delete. deleted returns the paths of the files deleted so far.
func filesAPIStub(t *testing.T) (prov *provider.OpenAIProvider, deleted func() []string) {
	t.Helper()
	var (
		mu      sync.Mutex
		deletes []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			mu.Lock()
			deletes = append(deletes, r.URL.Path)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "file-abc", "object": "file", "deleted": true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "file-abc", "object": "file", "bytes": 2, "created_at": 1, "filename": "table.csv", "purpose": "user_data"})
	}))
	t.Cleanup(srv.Close)
	client := openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(srv.URL))
	return provider.NewOpenAIProvider(nil, &client, nil, nil), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), deletes...)
	}
}

func TestCreateFileAttachment_StoresTheUploadAndReturnsTheRecord(t *testing.T) {
	prov, _ := filesAPIStub(t)
	userID, chatID := uuid.New(), uuid.New()
	attachmentID := uuid.New()
	store := &fakeStore{createFileAttachmentFn: func(_ context.Context, uid uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
		if uid != userID || fa.Name != "table.csv" || fa.FileID == nil || *fa.FileID != "file-abc" {
			t.Fatalf("unexpected attachment %+v for user %s", fa, uid)
		}
		fa.ID = attachmentID
		return &fa, nil
	}}
	h := NewHandler(store, zap.NewNop(), &agent.Agent{OpenAIProvider: prov}, HandlerConfig{})

	// A .csv is stored but not chunked, so no background work outlives the test.
	w := serveFileAttachmentWith(t, h, chatID.String(), &userID, "table.csv")

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	var got models.FileAttachment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != attachmentID {
		t.Fatalf("expected attachment %s, got %s", attachmentID, got.ID)
	}
}

func TestCreateFileAttachment_RollsBackTheProviderFileWhenTheRecordCannotBeSaved(t *testing.T) {
	prov, deleted := filesAPIStub(t)
	userID := uuid.New()
	// The default fakeStore refuses to create the record.
	h := NewHandler(&fakeStore{}, zap.NewNop(), &agent.Agent{OpenAIProvider: prov}, HandlerConfig{})

	w := serveFileAttachmentWith(t, h, uuid.NewString(), &userID, "table.csv")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	if got := deleted(); len(got) != 1 {
		t.Fatalf("expected the provider's copy to be deleted once, got %v", got)
	}
}
