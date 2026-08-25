package agentjob

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type fakeAgentJobProvider struct {
	job                     *models.AgentJob
	updatePromptCalled      bool
	lastOverridesPatch      models.SetAgentJobOverridesPatch
	setAgentJobOverridesErr error
}

func (f *fakeAgentJobProvider) CreateAgentJob(ctx context.Context, userID uuid.UUID, jobModel models.AgentJob) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobProvider) ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobProvider) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	if f.job == nil {
		return nil, errors.New("job not set")
	}
	return f.job, nil
}
func (f *fakeAgentJobProvider) UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobProvider) UpdateAgentJobPrompt(ctx context.Context, userID, id uuid.UUID, prompt string) (*models.AgentJob, error) {
	f.updatePromptCalled = true
	return nil, errors.New("unexpected call")
}
func (f *fakeAgentJobProvider) UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobProvider) UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobProvider) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobProvider) SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error) {
	f.lastOverridesPatch = patch
	if f.setAgentJobOverridesErr != nil {
		return nil, f.setAgentJobOverridesErr
	}
	if f.job == nil {
		return nil, errors.New("job not set")
	}
	return f.job, nil
}
func (f *fakeAgentJobProvider) DeleteAgentJob(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeAgentJobProvider) AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeAgentJobProvider) RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	return errors.New("not implemented")
}

func TestUpdateAgentJob_PromptCannotBeEmpty(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobProvider{
		job: &models.AgentJob{
			ID:       jobID,
			UserID:   userID,
			Prompt:   "existing prompt",
			Timezone: "UTC",
		},
	}

	h := NewHandler(provider, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPut, "/agent-job/"+jobID.String(), strings.NewReader(`{"prompt":"   "}`))
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	h.UpdateAgentJob(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if provider.updatePromptCalled {
		t.Fatalf("expected prompt update not to be called")
	}

	var resp models.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message != "prompt cannot be empty" {
		t.Fatalf("expected message %q, got %q", "prompt cannot be empty", resp.Message)
	}
}

func TestUpdateAgentJob_OverridesOnlyPersonalityDoesNotTouchModel(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	personalityID := uuid.New()
	modelID := uuid.New()
	provider := &fakeAgentJobProvider{
		job: &models.AgentJob{
			ID:            jobID,
			UserID:        userID,
			Prompt:        "existing prompt",
			Timezone:      "UTC",
			PersonalityID: &personalityID,
			ModelID:       &modelID,
		},
	}

	h := NewHandler(provider, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPut, "/agent-job/"+jobID.String(), strings.NewReader(`{"personality_id":""}`))
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	h.UpdateAgentJob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if !provider.lastOverridesPatch.UpdatePersonality {
		t.Fatalf("expected personality override to be updated")
	}
	if provider.lastOverridesPatch.UpdateModel {
		t.Fatalf("expected model override not to be updated when model_id omitted")
	}
	if provider.lastOverridesPatch.PersonalityID != nil {
		t.Fatalf("expected cleared personality_id")
	}
}

func TestUpdateAgentJob_InvalidOverridesReturns400(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobProvider{
		job: &models.AgentJob{
			ID:       jobID,
			UserID:   userID,
			Prompt:   "existing prompt",
			Timezone: "UTC",
		},
		setAgentJobOverridesErr: datastore.ErrInvalidRequestBody,
	}

	h := NewHandler(provider, nil, nil, zap.NewNop())

	pid := uuid.New()
	req := httptest.NewRequest(http.MethodPut, "/agent-job/"+jobID.String(), strings.NewReader(`{"personality_id":"`+pid.String()+`"}`))
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	h.UpdateAgentJob(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}
