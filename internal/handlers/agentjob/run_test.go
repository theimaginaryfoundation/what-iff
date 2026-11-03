package agentjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	var resp models.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message != "AgentJob scheduler is not active" {
		t.Fatalf("expected the inactive-scheduler message, got %q", resp.Message)
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

func runNowRequest(userID, jobID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/run", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String()})
	return req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
}

// Issue #427: a rate-limited manual run answers 429 job_run_limit_reached with the reset time,
// never the 503 "scheduler is not active" a down scheduler gets.
func TestRunAgentJobNow_RateLimitedReturnsTooManyRequests(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusActive},
	}
	retryAt := time.Now().Add(7 * time.Minute).UTC().Truncate(time.Second)
	runner := &fakeAgentJobRunner{err: &agentjobscheduler.RunLimitError{Limit: 10, Window: 30 * time.Minute, RetryAt: &retryAt}}
	h := NewHandler(provider, nil, runner, zap.NewNop())
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, runNowRequest(userID, jobID))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d: %s", http.StatusTooManyRequests, w.Code, w.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		Details struct {
			Limit         int        `json:"limit"`
			WindowSeconds int        `json:"window_seconds"`
			RetryAt       *time.Time `json:"retry_at"`
		} `json:"details"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if strings.Contains(strings.ToLower(resp.Message), "not active") {
		t.Fatalf("rate limit reported as an inactive scheduler: %q", resp.Message)
	}
	if resp.Code != models.ErrCodeJobRunLimitReached {
		t.Fatalf("expected code %q, got %q", models.ErrCodeJobRunLimitReached, resp.Code)
	}
	want := "Job run limit reached (10 runs per 30 minutes). Try again at " + retryAt.Format("15:04") + " UTC."
	if resp.Message != want {
		t.Fatalf("expected message %q, got %q", want, resp.Message)
	}
	if resp.Details.Limit != 10 || resp.Details.WindowSeconds != 1800 {
		t.Fatalf("unexpected details: %+v", resp.Details)
	}
	if resp.Details.RetryAt == nil || !resp.Details.RetryAt.Equal(retryAt) {
		t.Fatalf("expected retry_at %s, got %v", retryAt, resp.Details.RetryAt)
	}
	retryAfter, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || retryAfter < 400 || retryAfter > 421 {
		t.Fatalf("expected Retry-After of about 420s, got %q", w.Header().Get("Retry-After"))
	}
}

func TestRunAgentJobNow_RateLimitedWithoutResetTimeSaysLater(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	jobID := uuid.New()
	provider := &fakeAgentJobRunProvider{
		job: &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusPaused},
	}
	runner := &fakeAgentJobRunner{err: fmt.Errorf("wrapped: %w", &agentjobscheduler.RunLimitError{Limit: 10, Window: 30 * time.Minute})}
	h := NewHandler(provider, nil, runner, zap.NewNop())
	w := httptest.NewRecorder()

	h.RunAgentJobNow(w, runNowRequest(userID, jobID))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d: %s", http.StatusTooManyRequests, w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got != "" {
		t.Fatalf("expected no Retry-After without a reset time, got %q", got)
	}
	var resp models.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message != "Job run limit reached (10 runs per 30 minutes). Try again later." {
		t.Fatalf("unexpected message %q", resp.Message)
	}
	details, _ := resp.Details.(map[string]any)
	if _, ok := details["retry_at"]; ok {
		t.Fatalf("expected no retry_at, got %v", details)
	}
}
