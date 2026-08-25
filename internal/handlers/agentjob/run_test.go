package agentjob

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentjobscheduler "github.com/theimaginaryfoundation/what-iff/internal/agentjobs/scheduler"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type fakeAgentJobRunProvider struct {
	job    *models.AgentJob
	getErr error
}

func (f *fakeAgentJobRunProvider) CreateAgentJob(ctx context.Context, userID uuid.UUID, jobModel models.AgentJob) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.job, nil
}
func (f *fakeAgentJobRunProvider) UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) UpdateAgentJobPrompt(ctx context.Context, userID, id uuid.UUID, prompt string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) DeleteAgentJob(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeAgentJobRunProvider) RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	return errors.New("not implemented")
}

type fakeAgentJobRunner struct {
	called bool
	err    error
}

func (f *fakeAgentJobRunner) RunAgentJobNow(ctx context.Context, userID, id uuid.UUID) error {
	f.called = true
	return f.err
}

func TestRunAgentJobNow_SuccessActiveReturnsAccepted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusActive},
	}
	runner := &fakeAgentJobRunner{}
	h := NewHandler(provider, nil, runner, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, w.Code, w.Body.String())
	}
	if !runner.called {
		t.Fatalf("expected runner to be called")
	}

	var resp runAgentJobNowResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "triggered" {
		t.Fatalf("expected status %q, got %q", "triggered", resp.Status)
	}
}

func TestRunAgentJobNow_SuccessPausedReturnsAccepted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusPaused},
	}
	runner := &fakeAgentJobRunner{}
	h := NewHandler(provider, nil, runner, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, w.Code, w.Body.String())
	}
	if !runner.called {
		t.Fatalf("expected runner to be called")
	}
}

func TestRunAgentJobNow_InvalidStatusReturnsBadRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusComplete},
	}
	runner := &fakeAgentJobRunner{}
	h := NewHandler(provider, nil, runner, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if runner.called {
		t.Fatalf("expected runner not to be called")
	}
}

func TestRunAgentJobNow_SchedulerUnavailableReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusActive},
	}
	h := NewHandler(provider, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d: %s", http.StatusServiceUnavailable, w.Code, w.Body.String())
	}
}

func TestRunAgentJobNow_RunnerInactiveReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusActive},
	}
	runner := &fakeAgentJobRunner{err: agentjobscheduler.ErrSchedulerNotActive}
	h := NewHandler(provider, nil, runner, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d: %s", http.StatusServiceUnavailable, w.Code, w.Body.String())
	}
}

func TestRunAgentJobNow_NotFoundReturnsNotFound(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{getErr: datastore.ErrAgentJobNotFound}
	runner := &fakeAgentJobRunner{}
	h := NewHandler(provider, nil, runner, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
	if runner.called {
		t.Fatalf("expected runner not to be called")
	}
}
