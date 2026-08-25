package agentjob

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeRitualOpsProvider implements AgentJobProvider with configurable Add/Remove behavior.
type fakeRitualOpsProvider struct {
	addErr, removeErr error

	addCalls, removeCalls int
	lastUserID            uuid.UUID
	lastJobID             uuid.UUID
	lastRitualID          uuid.UUID
}

func (f *fakeRitualOpsProvider) CreateAgentJob(ctx context.Context, userID uuid.UUID, jobModel models.AgentJob) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) UpdateAgentJobPrompt(ctx context.Context, userID, id uuid.UUID, prompt string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) DeleteAgentJob(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeRitualOpsProvider) AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	f.addCalls++
	f.lastUserID = userID
	f.lastJobID = jobID
	f.lastRitualID = ritualID
	return f.addErr
}
func (f *fakeRitualOpsProvider) RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	f.removeCalls++
	f.lastUserID = userID
	f.lastJobID = jobID
	f.lastRitualID = ritualID
	return f.removeErr
}

func TestAddAgentJobRitual_SuccessNoContent(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d: %s", http.StatusNoContent, w.Code, w.Body.String())
	}
	if p.addCalls != 1 {
		t.Fatalf("expected AddAgentJobRitual called once, got %d", p.addCalls)
	}
	if p.lastUserID != userID || p.lastJobID != jobID || p.lastRitualID != ritualID {
		t.Fatalf("unexpected ids passed to provider")
	}
}

func TestAddAgentJobRitual_Unauthorized(t *testing.T) {
	t.Parallel()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, w.Code)
	}
	if p.addCalls != 0 {
		t.Fatalf("expected provider not called")
	}
}

func TestAddAgentJobRitual_InvalidJobID(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/bad/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": "not-a-uuid", "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if p.addCalls != 0 {
		t.Fatalf("expected provider not called")
	}
}

func TestAddAgentJobRitual_InvalidRitualID(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/rituals/bad", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": "not-a-uuid"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if p.addCalls != 0 {
		t.Fatalf("expected provider not called")
	}
}

func TestAddAgentJobRitual_NotFound(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{addErr: datastore.ErrAgentJobNotFound}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestAddAgentJobRitual_SkillNotFound(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{addErr: datastore.ErrRitualNotFound}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestAddAgentJobRitual_InternalError(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{addErr: errors.New("database error")}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.AddAgentJobRitual(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

func TestRemoveAgentJobRitual_SuccessNoContent(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.RemoveAgentJobRitual(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d: %s", http.StatusNoContent, w.Code, w.Body.String())
	}
	if p.removeCalls != 1 {
		t.Fatalf("expected RemoveAgentJobRitual called once, got %d", p.removeCalls)
	}
}

func TestRemoveAgentJobRitual_Unauthorized(t *testing.T) {
	t.Parallel()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	w := httptest.NewRecorder()
	h.RemoveAgentJobRitual(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, w.Code)
	}
	if p.removeCalls != 0 {
		t.Fatalf("expected provider not called")
	}
}

func TestRemoveAgentJobRitual_InvalidIDs(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	p := &fakeRitualOpsProvider{}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/agent-job/"+jobID.String()+"/rituals/bad", nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": "not-a-uuid"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.RemoveAgentJobRitual(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRemoveAgentJobRitual_NotFound(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{removeErr: datastore.ErrAgentJobNotFound}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.RemoveAgentJobRitual(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestRemoveAgentJobRitual_SkillNotFound(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{removeErr: datastore.ErrRitualNotFound}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.RemoveAgentJobRitual(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestRemoveAgentJobRitual_InternalError(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	jobID := uuid.New()
	ritualID := uuid.New()
	p := &fakeRitualOpsProvider{removeErr: errors.New("database error")}
	h := NewHandler(p, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/agent-job/"+jobID.String()+"/rituals/"+ritualID.String(), nil)
	req = mux.SetURLVars(req, map[string]string{"id": jobID.String(), "ritualId": ritualID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	h.RemoveAgentJobRitual(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}
