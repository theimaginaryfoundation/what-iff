package agentjob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/params"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

type fakeCreateAgentJobProvider struct {
	created *models.AgentJob
	lastJob models.AgentJob
}

func (f *fakeCreateAgentJobProvider) CreateAgentJob(ctx context.Context, userID uuid.UUID, jobModel models.AgentJob) (*models.AgentJob, error) {
	f.lastJob = jobModel
	if _, _, err := params.Prepare(jobModel.Prompt, jobModel.Parameters, jobModel.ParameterValues); err != nil {
		return nil, err
	}
	if f.created != nil {
		return f.created, nil
	}
	created := jobModel
	created.ID = uuid.New()
	created.UserID = userID
	return &created, nil
}

func (f *fakeCreateAgentJobProvider) ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) UpdateAgentJobParameters(ctx context.Context, userID, id uuid.UUID, patch models.AgentJobParametersPatch) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) DeleteAgentJob(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeCreateAgentJobProvider) RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	return errors.New("not implemented")
}

type fakeScheduleParser struct{}

func (f *fakeScheduleParser) ParseAgentJobSchedule(ctx context.Context, userID uuid.UUID, scheduleInput string, timezone string, now time.Time) (*models.AgentJobSchedulePreview, error) {
	runAt := now.Add(time.Hour)
	return &models.AgentJobSchedulePreview{
		ScheduleType: models.AgentJobScheduleTypeAt,
		RunAt:        &runAt,
		Timezone:     timezone,
		HumanSummary: "in one hour",
		NextRuns:     []time.Time{runAt},
	}, nil
}

func TestCreateAgentJob_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	modelID := uuid.New()
	provider := &fakeCreateAgentJobProvider{}
	handler := NewHandler(provider, &fakeScheduleParser{}, nil, zap.NewNop())

	body, err := json.Marshal(createAgentJobRequest{
		Title:         strPtr("Reminder"),
		Prompt:        "Check in on the task",
		ScheduleInput: "in an hour",
		Timezone:      strPtr("America/New_York"),
		ChatID:        strPtr(chatID.String()),
		ModelID:       strPtr(modelID.String()),
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/agent-job", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	handler.CreateAgentJob(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	if provider.lastJob.Prompt != "Check in on the task" {
		t.Fatalf("expected prompt to be stored")
	}
	if provider.lastJob.ChatID == nil || *provider.lastJob.ChatID != chatID {
		t.Fatalf("expected chat_id to be set")
	}
	if provider.lastJob.ModelID == nil || *provider.lastJob.ModelID != modelID {
		t.Fatalf("expected model_id to be set")
	}
}

func TestCreateAgentJob_RequiresPromptAndSchedule(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	handler := NewHandler(&fakeCreateAgentJobProvider{}, &fakeScheduleParser{}, nil, zap.NewNop())

	body, _ := json.Marshal(map[string]string{"prompt": "only prompt"})
	req := httptest.NewRequest(http.MethodPost, "/agent-job", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	handler.CreateAgentJob(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func strPtr(s string) *string { return &s }

func TestCreateAgentJob_Parameters(t *testing.T) {
	t.Parallel()

	post := func(t *testing.T, provider *fakeCreateAgentJobProvider, body string) *httptest.ResponseRecorder {
		t.Helper()
		handler := NewHandler(provider, &fakeScheduleParser{}, nil, zap.NewNop())
		req := httptest.NewRequest(http.MethodPost, "/agent-job", bytes.NewReader([]byte(body)))
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
		rec := httptest.NewRecorder()
		handler.CreateAgentJob(rec, req)
		return rec
	}

	t.Run("definitions and values reach the datastore", func(t *testing.T) {
		provider := &fakeCreateAgentJobProvider{}
		rec := post(t, provider, `{"prompt":"News on {{topics}}","schedule_input":"daily",
			"parameters":[{"name":"topics","type":"list"}],
			"parameter_values":{"topics":["AI","climate"]}}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
		}
		if len(provider.lastJob.Parameters) != 1 || provider.lastJob.Parameters[0].Type != modeltypes.JobParameterList {
			t.Fatalf("parameters = %+v", provider.lastJob.Parameters)
		}
		if v := provider.lastJob.ParameterValues["topics"]; !v.List || len(v.Items) != 2 {
			t.Fatalf("values = %+v", provider.lastJob.ParameterValues)
		}
	})

	t.Run("undefined placeholder is a 400 with details", func(t *testing.T) {
		rec := post(t, &fakeCreateAgentJobProvider{}, `{"prompt":"News on {{topics}}","schedule_input":"daily"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
		}
		var resp struct {
			Code    string `json:"code"`
			Details struct {
				Undefined []string `json:"undefined_placeholders"`
			} `json:"details"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Code != models.ErrCodeJobParametersInvalid || len(resp.Details.Undefined) != 1 || resp.Details.Undefined[0] != "topics" {
			t.Fatalf("response = %+v", resp)
		}
	})
}
