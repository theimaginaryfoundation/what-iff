package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// serveFileAttachment posts a multipart upload to the file-attachment route. A nil userID sends
// the request unauthenticated.
func serveFileAttachment(t *testing.T, chatID string, userID *uuid.UUID, fileName string) *httptest.ResponseRecorder {
	t.Helper()
	// An empty agent is enough for the cases that are rejected before the store is used.
	return serveFileAttachmentWith(t, NewHandler(&fakeStore{}, zap.NewNop(), &agent.Agent{}, HandlerConfig{}), chatID, userID, fileName)
}

func serveFileAttachmentWith(t *testing.T, h *Handler, chatID string, userID *uuid.UUID, fileName string) *httptest.ResponseRecorder {
	t.Helper()
	return serveFileAttachmentForm(t, h, chatID, userID, fileName, nil)
}

// serveFileAttachmentForm is serveFileAttachmentWith with extra form fields.
func serveFileAttachmentForm(t *testing.T, h *Handler, chatID string, userID *uuid.UUID, fileName string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
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

func TestCreateFileAttachment_StoresTheUploadAndReturnsTheRecord(t *testing.T) {
	userID, chatID := uuid.New(), uuid.New()
	attachmentID := uuid.New()
	store := &fakeStore{createFileAttachmentFn: func(_ context.Context, uid uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
		// No provider holds a copy of the file, so the record carries no provider file id.
		if uid != userID || fa.Name != "table.csv" || fa.FileID != nil {
			t.Fatalf("unexpected attachment %+v for user %s", fa, uid)
		}
		fa.ID = attachmentID
		return &fa, nil
	}}
	h := NewHandler(store, zap.NewNop(), &agent.Agent{}, HandlerConfig{})

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

func TestCreateFileAttachment_StoresADescription(t *testing.T) {
	userID, chatID := uuid.New(), uuid.New()
	var stored *string
	store := &fakeStore{createFileAttachmentFn: func(_ context.Context, _ uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
		stored = fa.Description
		fa.ID = uuid.New()
		return &fa, nil
	}}
	h := NewHandler(store, zap.NewNop(), &agent.Agent{}, HandlerConfig{})

	w := serveFileAttachmentForm(t, h, chatID.String(), &userID, "table.csv", map[string]string{"description": "  Mask for photo.png  "})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	if stored == nil || *stored != "Mask for photo.png" {
		t.Fatalf("expected the trimmed description to be stored, got %v", stored)
	}

	w = serveFileAttachmentForm(t, h, chatID.String(), &userID, "table.csv", map[string]string{"description": strings.Repeat("x", 501)})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected a too-long description to be refused, got %d", w.Code)
	}
}

func TestCreateFileAttachment_ReportsAFailureToSaveTheRecord(t *testing.T) {
	userID := uuid.New()
	// The default fakeStore refuses to create the record.
	h := NewHandler(&fakeStore{}, zap.NewNop(), &agent.Agent{}, HandlerConfig{})

	w := serveFileAttachmentWith(t, h, uuid.NewString(), &userID, "table.csv")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	if !strings.Contains(w.Body.String(), "Error creating file attachment") {
		t.Fatalf("expected the record failure message, got %q", w.Body.String())
	}
}
