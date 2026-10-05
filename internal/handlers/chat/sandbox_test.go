package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// sandboxRequest sends one request to a handler wired to a fake store that records the chat the
// handler passes on, and returns it.
func sandboxRequest(t *testing.T, method, path, body string) (code int, saved models.Chat) {
	t.Helper()
	userID, chatID := uuid.New(), uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Original", Archived: boolPtr(false)}
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) { return existing, nil },
		updateChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
			saved = chat
			return &chat, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	path = strings.ReplaceAll(path, "{id}", chatID.String())
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, saved
}

func TestPatchChat_Sandboxed(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		body         string
		wantSet      bool
		wantSandbox  bool
		wantStatus   int
		wantNoUpdate bool
	}{
		"turning it on is an explicit change":  {body: `{"sandboxed":true}`, wantSet: true, wantSandbox: true, wantStatus: http.StatusOK},
		"turning it off is an explicit change": {body: `{"sandboxed":false}`, wantSet: true, wantSandbox: false, wantStatus: http.StatusOK},
		"omitting it leaves the flag alone":    {body: `{"name":"Renamed"}`, wantSet: false, wantStatus: http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, saved := sandboxRequest(t, http.MethodPatch, "/chat/{id}", tc.body)
			if code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", code, tc.wantStatus)
			}
			if saved.SetSandboxed != tc.wantSet || saved.Sandboxed != tc.wantSandbox {
				t.Fatalf("saved SetSandboxed=%v Sandboxed=%v, want %v/%v", saved.SetSandboxed, saved.Sandboxed, tc.wantSet, tc.wantSandbox)
			}
		})
	}
}

func TestUpdateChat_Sandboxed(t *testing.T) {
	t.Parallel()
	code, saved := sandboxRequest(t, http.MethodPut, "/chat/{id}", `{"name":"Renamed","sandboxed":true}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !saved.SetSandboxed || !saved.Sandboxed {
		t.Fatalf("saved SetSandboxed=%v Sandboxed=%v, want an explicit sandbox", saved.SetSandboxed, saved.Sandboxed)
	}

	_, saved = sandboxRequest(t, http.MethodPut, "/chat/{id}", `{"name":"Renamed"}`)
	if saved.SetSandboxed {
		t.Fatalf("a PUT without the flag must not write it")
	}
}

func TestCreateChat_Sandboxed(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		extra string
		want  bool
	}{
		"sandboxed":     {extra: `,"sandboxed":true`, want: true},
		"not sandboxed": {extra: ``, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var captured models.Chat
			store := &createChatStore{
				createChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
					captured = chat
					return &models.Chat{ID: uuid.New(), Name: chat.Name, Sandboxed: chat.Sandboxed}, nil
				},
			}
			h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
			router := mux.NewRouter()
			router.HandleFunc("/chat", h.CreateChat).Methods(http.MethodPost)

			body := `{"name":"Discord thread","model_id":"` + uuid.NewString() + `"` + tc.extra + `}`
			req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if captured.Sandboxed != tc.want {
				t.Fatalf("Sandboxed passed to the datastore = %v, want %v", captured.Sandboxed, tc.want)
			}
			if !strings.Contains(w.Body.String(), `"sandboxed":`+map[bool]string{true: "true", false: "false"}[tc.want]) {
				t.Fatalf("the response should carry the flag: %s", w.Body.String())
			}
		})
	}
}
