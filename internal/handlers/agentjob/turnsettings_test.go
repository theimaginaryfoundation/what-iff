package agentjob

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

func updateJob(t *testing.T, body string) (*fakeAgentJobProvider, *httptest.ResponseRecorder) {
	t.Helper()
	userID, jobID := uuid.New(), uuid.New()
	provider := &fakeAgentJobProvider{job: &models.AgentJob{ID: jobID, UserID: userID, Prompt: "p", Timezone: "UTC"}}
	h := NewHandler(provider, nil, nil, zap.NewNop())
	req := httptest.NewRequest(http.MethodPut, "/agent-job/"+jobID.String(), strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.UpdateAgentJob(w, req)
	return provider, w
}

// A job's reasoning effort and agent loop (ADR 0x026) update independently; "" clears.
func TestUpdateAgentJob_TurnSettings(t *testing.T) {
	t.Parallel()
	provider, w := updateJob(t, `{"reasoning_effort":"high"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	p := provider.lastOverridesPatch
	if !p.UpdateReasoningEffort || p.UpdateAgentLoop || p.UpdatePersonality || p.UpdateModel {
		t.Fatalf("only the effort should change: %+v", p)
	}
	if p.TurnSettings.ReasoningEffort == nil || *p.TurnSettings.ReasoningEffort != models.ReasoningEffortHigh {
		t.Fatalf("effort = %v", p.TurnSettings.ReasoningEffort)
	}

	provider, w = updateJob(t, `{"agent_loop":""}`)
	if w.Code != http.StatusOK || !provider.lastOverridesPatch.UpdateAgentLoop || provider.lastOverridesPatch.TurnSettings.AgentLoop != nil {
		t.Fatalf("clearing the loop: status %d, patch %+v", w.Code, provider.lastOverridesPatch)
	}

	for _, body := range []string{`{"reasoning_effort":"max"}`, `{"agent_loop":"react"}`} {
		if _, w := updateJob(t, body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d", body, w.Code)
		}
	}
}

func createJob(t *testing.T, body string) (*fakeCreateAgentJobProvider, *httptest.ResponseRecorder) {
	t.Helper()
	provider := &fakeCreateAgentJobProvider{}
	h := NewHandler(provider, &fakeScheduleParser{}, nil, zap.NewNop())
	req := httptest.NewRequest(http.MethodPost, "/agent-job", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	h.CreateAgentJob(w, req)
	return provider, w
}

func TestCreateAgentJob_TurnSettings(t *testing.T) {
	t.Parallel()
	provider, w := createJob(t, `{"prompt":"p","schedule_input":"in an hour","reasoning_effort":"low","agent_loop":"advanced"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	got := provider.lastJob.TurnSettingValues
	if got.ReasoningEffort == nil || *got.ReasoningEffort != models.ReasoningEffortLow || got.AgentLoop == nil || *got.AgentLoop != models.AgentLoopAdvanced {
		t.Fatalf("turn settings not passed through: %+v", got)
	}
	if _, w := createJob(t, `{"prompt":"p","schedule_input":"in an hour","reasoning_effort":"max"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid effort: status %d", w.Code)
	}
}
