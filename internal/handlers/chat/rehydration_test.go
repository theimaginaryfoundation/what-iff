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
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestNeedsRehydration(t *testing.T) {
	t.Parallel()
	str := func(s string) *string { return &s }
	flag := func(b bool) *bool { return &b }

	cases := []struct {
		name string
		chat models.Chat
		want bool
	}{
		{"imported, unarchived, not yet summarized", models.Chat{Source: str("openai"), Archived: flag(false)}, true},
		{"previous attempt failed", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStateFailed}, true},
		{"native thread", models.Chat{Archived: flag(false)}, false},
		{"empty source", models.Chat{Source: str(""), Archived: flag(false)}, false},
		// An archived thread opens read-only; summarizing waits until it is restored and opened.
		{"archived", models.Chat{Source: str("openai"), Archived: flag(true)}, false},
		{"already pending", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStatePending}, false},
		{"already processing", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStateProcessing}, false},
		{"already ready", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStateReady}, false},
		{"already has a checkpoint", models.Chat{Source: str("openai"), Archived: flag(false), CheckpointSummary: "summary"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := needsRehydration(&tc.chat); got != tc.want {
				t.Fatalf("needsRehydration = %v, want %v", got, tc.want)
			}
		})
	}
}

func rehydrateRequest(t *testing.T, store *fakeStore, userID uuid.UUID, id string, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+id+"/rehydrate", nil)
	if authed {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRehydrateChat_ReturnsTheChat(t *testing.T) {
	t.Parallel()
	userID, chatID := uuid.New(), uuid.New()
	store := &fakeStore{getChatFn: func(_ context.Context, uid, id uuid.UUID) (*models.Chat, error) {
		if uid != userID || id != chatID {
			t.Fatalf("unexpected lookup %s/%s", uid, id)
		}
		return &models.Chat{ID: id, Name: "Imported"}, nil
	}}

	// No agent is configured in this handler, so nothing starts; the chat comes back as it is.
	if w := rehydrateRequest(t, store, userID, chatID.String(), true); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRehydrateChat_Errors(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	notFound := &fakeStore{getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
		return nil, datastore.ErrChatNotFound
	}}
	broken := &fakeStore{getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
		return nil, errors.New("boom")
	}}

	for name, tc := range map[string]struct {
		store  *fakeStore
		id     string
		authed bool
		want   int
	}{
		"unauthorized":    {notFound, uuid.NewString(), false, http.StatusUnauthorized},
		"invalid chat id": {notFound, "not-a-uuid", true, http.StatusBadRequest},
		"chat not found":  {notFound, uuid.NewString(), true, http.StatusNotFound},
		"store failure":   {broken, uuid.NewString(), true, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if w := rehydrateRequest(t, tc.store, userID, tc.id, tc.authed); w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}
