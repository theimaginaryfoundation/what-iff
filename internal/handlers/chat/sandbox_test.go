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

// sandboxRequest sends one request to a handler wired to a fake store whose chat has the given
// scope, records the chat the handler passes on, and returns it.
func sandboxRequest(t *testing.T, method, path, body string, existingScope models.ContextScope) (code int, saved models.Chat, response string) {
	t.Helper()
	userID, chatID := uuid.New(), uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Original", Archived: boolPtr(false), ContextScope: existingScope}
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
	return w.Code, saved, w.Body.String()
}

// A chat is sandboxed when it is created, never later: PUT and PATCH accept "account" (leaving the
// sandbox is an explicit change) and refuse "sandbox" on a chat that is not one already.
func TestUpdateAndPatchChat_ContextScope(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPatch, http.MethodPut} {
		for name, tc := range map[string]struct {
			existing   models.ContextScope
			body       string
			wantStatus int
			wantSet    bool
			wantScope  models.ContextScope
		}{
			"leaving the sandbox is an explicit change": {existing: models.ContextScopeSandbox, body: `{"name":"Renamed","context_scope":"account"}`, wantStatus: http.StatusOK, wantSet: true, wantScope: models.ContextScopeAccount},
			"restating sandbox on a sandbox is allowed": {existing: models.ContextScopeSandbox, body: `{"name":"Renamed","context_scope":"sandbox"}`, wantStatus: http.StatusOK, wantSet: true, wantScope: models.ContextScopeSandbox},
			"sandboxing an existing chat is refused":    {existing: models.ContextScopeAccount, body: `{"name":"Renamed","context_scope":"sandbox"}`, wantStatus: http.StatusBadRequest},
			"an unknown scope is refused":               {existing: models.ContextScopeAccount, body: `{"name":"Renamed","context_scope":"project"}`, wantStatus: http.StatusBadRequest},
			"omitting it leaves the scope alone":        {existing: models.ContextScopeSandbox, body: `{"name":"Renamed"}`, wantStatus: http.StatusOK, wantSet: false},
		} {
			t.Run(method+" "+name, func(t *testing.T) {
				t.Parallel()
				code, saved, body := sandboxRequest(t, method, "/chat/{id}", tc.body, tc.existing)
				if code != tc.wantStatus {
					t.Fatalf("status = %d, want %d: %s", code, tc.wantStatus, body)
				}
				if code != http.StatusOK {
					if saved.SetContextScope {
						t.Fatalf("a refused request must not reach the datastore")
					}
					if strings.Contains(tc.body, `"sandbox"`) && !strings.Contains(body, "when it is created") {
						t.Fatalf("the refusal should say why: %s", body)
					}
					return
				}
				if saved.SetContextScope != tc.wantSet || (tc.wantSet && saved.ContextScope != tc.wantScope) {
					t.Fatalf("saved SetContextScope=%v ContextScope=%q, want %v/%q", saved.SetContextScope, saved.ContextScope, tc.wantSet, tc.wantScope)
				}
			})
		}
	}
}

func TestCreateChat_ContextScope(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		extra      string
		want       models.ContextScope
		wantStatus int
	}{
		"sandboxed":     {extra: `,"context_scope":"sandbox"`, want: models.ContextScopeSandbox, wantStatus: http.StatusCreated},
		"account":       {extra: `,"context_scope":"account"`, want: models.ContextScopeAccount, wantStatus: http.StatusCreated},
		"default":       {extra: ``, want: "", wantStatus: http.StatusCreated},
		"unknown scope": {extra: `,"context_scope":"project"`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var captured models.Chat
			store := &createChatStore{
				createChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
					captured = chat
					return &models.Chat{ID: uuid.New(), Name: chat.Name, ContextScope: chat.ContextScope.OrDefault()}, nil
				},
			}
			h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
			router := mux.NewRouter()
			router.HandleFunc("/chat", h.CreateChat).Methods(http.MethodPost)

			body := `{"name":"Discord thread","model_id":"` + uuid.NewString() + `","disabled_tools":["list"]` + tc.extra + `}`
			req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if w.Code != http.StatusCreated {
				return
			}
			if captured.ContextScope != tc.want {
				t.Fatalf("ContextScope passed to the datastore = %q, want %q", captured.ContextScope, tc.want)
			}
			if captured.DisabledTools != nil {
				t.Fatalf("disabled_tools is not part of create; the datastore decides a sandbox's defaults")
			}
			if !strings.Contains(w.Body.String(), `"context_scope":"`+string(tc.want.OrDefault())+`"`) {
				t.Fatalf("the response should carry the scope: %s", w.Body.String())
			}
		})
	}
}
