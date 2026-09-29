package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMarkAllChatsRead(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tests := []struct {
		name       string
		withUser   bool
		storeErr   error
		wantStatus int
		wantBody   string
	}{
		{name: "success", withUser: true, wantStatus: http.StatusOK, wantBody: `"updated_count":7`},
		{name: "unauthorized", withUser: false, wantStatus: http.StatusUnauthorized},
		{name: "store error is not reported as success", withUser: true, storeErr: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotUser uuid.UUID
			store := &fakeStore{
				markAllChatMessagesReadFn: func(ctx context.Context, uid uuid.UUID) (int, error) {
					gotUser = uid
					if tt.storeErr != nil {
						return 0, tt.storeErr
					}
					return 7, nil
				},
			}

			h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
			router := mux.NewRouter()
			h.RegisterRoutes(router)

			req := httptest.NewRequest(http.MethodPost, "/chat/mark-all-read", nil)
			if tt.withUser {
				req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (%s)", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.withUser && gotUser != userID {
				t.Fatalf("store called with user %s, want %s", gotUser, userID)
			}
			if tt.wantBody != "" && !strings.Contains(w.Body.String(), tt.wantBody) {
				t.Fatalf("body %q does not contain %q", w.Body.String(), tt.wantBody)
			}
		})
	}
}
