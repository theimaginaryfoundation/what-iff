package personality

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// An unknown reasoning effort or agent loop (ADR 0x026) is refused before anything is saved, on
// create and on both update handlers.
func TestPersonalityHandlers_RejectUnknownTurnSettings(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		listPersonalitiesFn: func(context.Context, uuid.UUID, int, int, models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}}, nil
		},
		createPersonalityFn: func(context.Context, uuid.UUID, models.Personality) (*models.Personality, error) {
			t.Fatalf("nothing should be saved")
			return nil, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	id := uuid.New()

	send := func(method, path string, handler http.HandlerFunc, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"id": id.String()})
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
		w := httptest.NewRecorder()
		if handler != nil {
			handler(w, req)
		} else {
			router.ServeHTTP(w, req)
		}
		return w.Code, w.Body.String()
	}
	expectRejected := func(code int, resp, field string) {
		t.Helper()
		require.Equal(t, http.StatusBadRequest, code, resp)
		require.Contains(t, resp, field, "rejected for the turn setting, not something else")
	}
	body := `{"name":"Vix","system_prompt":"You are Vix.","reasoning_effort":"max"}`
	code, resp := send(http.MethodPost, "/personality", nil, body)
	expectRejected(code, resp, "reasoning_effort")
	code, resp = send(http.MethodPut, "/personality/"+id.String(), nil, `{"name":"Vix","system_prompt":"You are Vix.","agent_loop":"react"}`)
	expectRejected(code, resp, "agent_loop")
	code, resp = send(http.MethodPut, "/personality/"+id.String(), h.UpdatePersonality, body)
	expectRejected(code, resp, "reasoning_effort")
}
