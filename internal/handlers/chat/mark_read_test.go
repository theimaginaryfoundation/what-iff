package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

func TestMarkChatRead_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()

	called := false
	store := &fakeStore{
		markChatMessagesReadFn: func(ctx context.Context, uid, cid uuid.UUID) (int, error) {
			called = true
			if uid != userID {
				t.Fatalf("unexpected user id: got %s want %s", uid, userID)
			}
			if cid != chatID {
				t.Fatalf("unexpected chat id: got %s want %s", cid, chatID)
			}
			return 3, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/mark-read", nil)
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if !called {
		t.Fatalf("expected MarkChatMessagesRead to be called")
	}
}

func TestMarkChatRead_InvalidChatID(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeStore{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/not-a-uuid/mark-read", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "not-a-uuid"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestMarkChatRead_Unauthorized(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	store := &fakeStore{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/mark-read", nil)
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestMarkChatRead_NotFound(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	store := &fakeStore{
		markChatMessagesReadFn: func(ctx context.Context, uid, cid uuid.UUID) (int, error) {
			return 0, datastore.ErrChatNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/mark-read", nil)
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestMarkChatRead_InternalError(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	store := &fakeStore{
		markChatMessagesReadFn: func(ctx context.Context, uid, cid uuid.UUID) (int, error) {
			return 0, errors.New("boom")
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/mark-read", nil)
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}
