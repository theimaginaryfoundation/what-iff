package mood

import (
	"context"
	"errors"
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

// fakeStore records the mood requests the handlers pass on; other methods are unused here.
type fakeStore struct {
	Store
	created *models.CreateMoodRequest
	updated *models.UpdateMoodRequest
}

func (f *fakeStore) CreateMood(_ context.Context, _ uuid.UUID, req models.CreateMoodRequest) (*models.Mood, error) {
	f.created = &req
	return &models.Mood{ID: uuid.New(), Name: req.Name, TurnSettingValues: req.TurnSettingValues}, nil
}

func (f *fakeStore) UpdateMood(_ context.Context, _ uuid.UUID, id uuid.UUID, req models.UpdateMoodRequest) (*models.Mood, error) {
	f.updated = &req
	return &models.Mood{ID: id, Name: req.Name, TurnSettingValues: req.TurnSettingValues}, nil
}

func (f *fakeStore) GetMood(context.Context, uuid.UUID, uuid.UUID) (*models.Mood, error) {
	return nil, errors.New("unused")
}

func moodRequest(t *testing.T, method, body string, handle func(*Handler) http.HandlerFunc) (*fakeStore, *httptest.ResponseRecorder) {
	t.Helper()
	store := &fakeStore{}
	h := NewHandler(store, zap.NewNop(), nil)
	id := uuid.New()
	req := httptest.NewRequest(method, "/mood/"+id.String(), strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": id.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	handle(h)(w, req)
	return store, w
}

// A mode's reasoning effort and agent loop (ADR 0x026) are validated and passed on; "" is "not set".
func TestMoodHandlers_TurnSettings(t *testing.T) {
	create := func(h *Handler) http.HandlerFunc { return h.CreateMood }
	update := func(h *Handler) http.HandlerFunc { return h.UpdateMood }

	store, w := moodRequest(t, http.MethodPost, `{"name":"Research","reasoning_effort":"high","agent_loop":""}`, create)
	if w.Code >= 300 {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}
	if store.created.ReasoningEffort == nil || *store.created.ReasoningEffort != models.ReasoningEffortHigh || store.created.AgentLoop != nil {
		t.Fatalf("create passed %+v", store.created.TurnSettingValues)
	}

	store, w = moodRequest(t, http.MethodPut, `{"name":"Research","agent_loop":"advanced"}`, update)
	if w.Code >= 300 {
		t.Fatalf("update status %d: %s", w.Code, w.Body.String())
	}
	if store.updated.AgentLoop == nil || *store.updated.AgentLoop != models.AgentLoopAdvanced || store.updated.ReasoningEffort != nil {
		t.Fatalf("update passed %+v", store.updated.TurnSettingValues)
	}

	for _, handle := range []func(*Handler) http.HandlerFunc{create, update} {
		store, w := moodRequest(t, http.MethodPost, `{"name":"x","reasoning_effort":"max"}`, handle)
		if w.Code != http.StatusBadRequest || store.created != nil || store.updated != nil {
			t.Fatalf("invalid effort: status %d", w.Code)
		}
	}
}
