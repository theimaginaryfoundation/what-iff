package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// PUT and PATCH change the thread's reasoning effort and agent loop (ADR 0x026) only when sent,
// mark each as an explicit change, clear on "", and refuse unknown values.
func TestUpdateAndPatchChat_TurnSettings(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPatch, http.MethodPut} {
		for name, tc := range map[string]struct {
			body       string
			wantStatus int
			wantEffort *models.ReasoningEffort
			setEffort  bool
			wantLoop   *models.AgentLoop
			setLoop    bool
		}{
			"sets both": {body: `{"name":"x","reasoning_effort":"high","agent_loop":"advanced"}`, wantStatus: http.StatusOK,
				wantEffort: ptr(models.ReasoningEffortHigh), setEffort: true, wantLoop: ptr(models.AgentLoopAdvanced), setLoop: true},
			"empty clears":         {body: `{"name":"x","reasoning_effort":""}`, wantStatus: http.StatusOK, setEffort: true},
			"omitted is untouched": {body: `{"name":"x"}`, wantStatus: http.StatusOK},
			"unknown effort":       {body: `{"name":"x","reasoning_effort":"max"}`, wantStatus: http.StatusBadRequest},
			"unknown loop":         {body: `{"name":"x","agent_loop":"react"}`, wantStatus: http.StatusBadRequest},
		} {
			t.Run(method+" "+name, func(t *testing.T) {
				t.Parallel()
				code, saved, body := sandboxRequest(t, method, "/chat/{id}", tc.body, models.ContextScopeAccount)
				if code != tc.wantStatus {
					t.Fatalf("status = %d, want %d: %s", code, tc.wantStatus, body)
				}
				if code != http.StatusOK {
					return
				}
				if saved.SetReasoningEffort != tc.setEffort || saved.SetAgentLoop != tc.setLoop {
					t.Fatalf("set flags effort=%v loop=%v, want %v/%v", saved.SetReasoningEffort, saved.SetAgentLoop, tc.setEffort, tc.setLoop)
				}
				if tc.setEffort && !equalPtr(saved.ReasoningEffort, tc.wantEffort) {
					t.Fatalf("reasoning effort = %v, want %v", saved.ReasoningEffort, tc.wantEffort)
				}
				if tc.setLoop && !equalPtr(saved.AgentLoop, tc.wantLoop) {
					t.Fatalf("agent loop = %v, want %v", saved.AgentLoop, tc.wantLoop)
				}
			})
		}
	}
}

func ptr[T any](v T) *T { return &v }

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// POST /chat passes valid turn settings to the datastore (which seeds the rest) and refuses unknown ones.
func TestCreateChat_TurnSettings(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		body       string
		wantStatus int
		wantEffort *models.ReasoningEffort
	}{
		"valid":   {body: `{"name":"x","reasoning_effort":"medium"}`, wantStatus: http.StatusCreated, wantEffort: ptr(models.ReasoningEffortMedium)},
		"empty":   {body: `{"name":"x","reasoning_effort":""}`, wantStatus: http.StatusCreated},
		"unknown": {body: `{"name":"x","reasoning_effort":"max"}`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var captured models.Chat
			store := &createChatStore{createChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
				captured = chat
				return &models.Chat{ID: uuid.New(), Name: chat.Name}, nil
			}}
			h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
			req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(tc.body))
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
			w := httptest.NewRecorder()
			h.CreateChat(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if w.Code == http.StatusCreated && !equalPtr(captured.ReasoningEffort, tc.wantEffort) {
				t.Fatalf("effort passed = %v, want %v", captured.ReasoningEffort, tc.wantEffort)
			}
		})
	}
}
