package personality

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakePersonalityAgent struct {
	enqueuePersonalityGenerationJobFn func(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error)
}

func (f *fakePersonalityAgent) EnqueueExpressionGridJob(ctx context.Context, userID, personalityID uuid.UUID) (*models.Job, error) {
	return nil, errors.New("not implemented")
}

func (f *fakePersonalityAgent) EnqueuePersonalityPortraitJob(ctx context.Context, userID, flowID uuid.UUID, systemPrompt, imageStyle string) (*models.Job, error) {
	return nil, errors.New("not implemented")
}

func (f *fakePersonalityAgent) EnqueuePersonalityGenerationJob(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error) {
	if f.enqueuePersonalityGenerationJobFn != nil {
		return f.enqueuePersonalityGenerationJobFn(ctx, userID, flowID)
	}
	return nil, errors.New("not implemented")
}

func TestGetOrCreateFlow_ReturnsExistingFlow(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()

	store := &fakeStore{
		getOrCreateActiveFlowFn: func(ctx context.Context, uid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:          flowID,
				Status:      "in_progress",
				CurrentStep: 2,
				Answers:     map[string]string{"name": "Sparky"},
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/personality/generate", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp models.PersonalityGenFlow
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != flowID {
		t.Fatalf("expected flow id %s, got %s", flowID, resp.ID)
	}
	if resp.CurrentStep != 2 {
		t.Fatalf("expected current_step 2, got %d", resp.CurrentStep)
	}
}

func TestGetFlow_ReturnsFlowByID(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			if uid != userID || fid != flowID {
				t.Fatalf("unexpected ids user=%s flow=%s", uid, fid)
			}
			return &models.PersonalityGenFlow{
				ID:          fid,
				Status:      "in_progress",
				CurrentStep: 1,
				Answers:     map[string]string{"general_description": "curious"},
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/personality/generate/"+flowID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp models.PersonalityGenFlow
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != flowID {
		t.Fatalf("expected flow id %s, got %s", flowID, resp.ID)
	}
}

func TestUpdateFlow_SavesProgress(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()

	var savedStep int
	var savedAnswers map[string]string

	store := &fakeStore{
		updateFlowFn: func(ctx context.Context, uid uuid.UUID, fid uuid.UUID, req models.UpdateFlowRequest) (*models.PersonalityGenFlow, error) {
			savedStep = req.CurrentStep
			savedAnswers = req.Answers
			return &models.PersonalityGenFlow{
				ID:          fid,
				Status:      "in_progress",
				CurrentStep: req.CurrentStep,
				Answers:     req.Answers,
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"current_step": 3, "answers": {"name": "Sparky", "vibe": "playful"}}`
	req := httptest.NewRequest(http.MethodPut, "/personality/generate/"+flowID.String(), strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if savedStep != 3 {
		t.Fatalf("expected saved step 3, got %d", savedStep)
	}
	if savedAnswers["name"] != "Sparky" {
		t.Fatalf("expected answer 'Sparky', got %q", savedAnswers["name"])
	}
}

func TestResetFlow_ReturnsFreshFlow(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	oldFlowID := uuid.New()
	newFlowID := uuid.New()

	store := &fakeStore{
		resetFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			if uid != userID || fid != oldFlowID {
				t.Fatalf("unexpected reset ids user=%s flow=%s", uid, fid)
			}
			return &models.PersonalityGenFlow{
				ID:          newFlowID,
				Status:      "in_progress",
				CurrentStep: 0,
				Answers:     map[string]string{},
				ImageStyle:  "auto",
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+oldFlowID.String()+"/reset", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp models.PersonalityGenFlow
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != newFlowID {
		t.Fatalf("expected new flow id %s, got %s", newFlowID, resp.ID)
	}
	if resp.CurrentStep != 0 {
		t.Fatalf("expected current_step 0, got %d", resp.CurrentStep)
	}
}

func TestResetFlow_ActiveGenerationJob_ReturnsConflict(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	jobID := uuid.New()

	store := &fakeStore{
		resetFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return nil, datastore.ErrFlowGenerationJobAlreadyActive
		},
		findActivePersonalityGenerationJobFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.Job, error) {
			return &models.Job{
				ID:        jobID,
				UserID:    uid,
				JobType:   "personality_generation",
				Reference: fid.String(),
				Status:    models.JobStatusProcessing,
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/reset", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected %d, got %d: %s", http.StatusConflict, w.Code, w.Body.String())
	}

	var resp models.PersonalityMediaJobConflict
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Active.JobID != jobID.String() {
		t.Fatalf("expected active job id %s, got %s", jobID, resp.Active.JobID)
	}
}

func TestCompleteFlow_EnqueuesGenerationJob(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	jobID := uuid.New()

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:      fid,
				Status:  "in_progress",
				Answers: map[string]string{"general_description": "helpful fox"},
			}, nil
		},
	}
	agent := &fakePersonalityAgent{
		enqueuePersonalityGenerationJobFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.Job, error) {
			if uid != userID || fid != flowID {
				t.Fatalf("unexpected enqueue ids user=%s flow=%s", uid, fid)
			}
			return &models.Job{ID: jobID, JobType: "personality_generation"}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	h.personalityAgent = agent
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/complete", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d: %s", http.StatusAccepted, w.Code, w.Body.String())
	}

	var resp models.PersonalityMediaJobResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.JobID != jobID.String() {
		t.Fatalf("expected job id %s, got %s", jobID, resp.JobID)
	}
	if resp.JobType != "personality_generation" {
		t.Fatalf("expected job type personality_generation, got %s", resp.JobType)
	}
}

func TestCompleteFlow_ConflictReturnsActiveJob(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	activeJobID := uuid.New()
	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:      fid,
				Status:  "in_progress",
				Answers: map[string]string{"general_description": "helpful fox"},
			}, nil
		},
	}
	agent := &fakePersonalityAgent{
		enqueuePersonalityGenerationJobFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.Job, error) {
			return nil, &agent.ErrPersonalityMediaJobActive{
				Job: &models.Job{
					ID:        activeJobID,
					UserID:    uid,
					JobType:   "personality_generation",
					Reference: fid.String(),
					Status:    models.JobStatusProcessing,
				},
			}
		},
	}
	h := NewHandler(store, zap.NewNop(), nil)
	h.personalityAgent = agent
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/complete", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected %d, got %d: %s", http.StatusConflict, w.Code, w.Body.String())
	}

	var resp models.PersonalityMediaJobConflict
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Active.JobID != activeJobID.String() {
		t.Fatalf("expected active job id %s, got %s", activeJobID, resp.Active.JobID)
	}
}

func TestRegenerateFlow_PreparesThenEnqueues(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	jobID := uuid.New()
	updateCalled := false

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:               fid,
				Status:           "generated",
				CurrentStep:      3,
				Answers:          map[string]string{"general_description": "helpful fox"},
				ImageStyle:       "auto",
				ReferenceImageID: nil,
			}, nil
		},
		updateFlowFn: func(ctx context.Context, uid uuid.UUID, fid uuid.UUID, req models.UpdateFlowRequest) (*models.PersonalityGenFlow, error) {
			updateCalled = true
			if req.CurrentStep != 3 {
				t.Fatalf("expected step 3, got %d", req.CurrentStep)
			}
			return &models.PersonalityGenFlow{
				ID:               fid,
				Status:           "in_progress",
				CurrentStep:      req.CurrentStep,
				Answers:          req.Answers,
				ImageStyle:       req.ImageStyle,
				ReferenceImageID: req.ReferenceImageID,
			}, nil
		},
	}
	agent := &fakePersonalityAgent{
		enqueuePersonalityGenerationJobFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.Job, error) {
			return &models.Job{ID: jobID, JobType: "personality_generation"}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	h.personalityAgent = agent
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/regenerate", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d: %s", http.StatusAccepted, w.Code, w.Body.String())
	}
	if !updateCalled {
		t.Fatalf("expected flow prepare update before enqueue")
	}
}

func TestGetActiveGenerationJob_ReturnsJob(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	jobID := uuid.New()
	store := &fakeStore{
		findActivePersonalityGenerationJobFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.Job, error) {
			return &models.Job{
				ID:        jobID,
				UserID:    uid,
				JobType:   "personality_generation",
				Reference: fid.String(),
				Status:    models.JobStatusProcessing,
			}, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/personality/generate/"+flowID.String()+"/active-job", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp models.ActivePersonalityMediaJob
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.JobID != jobID.String() {
		t.Fatalf("expected job id %s, got %s", jobID, resp.JobID)
	}
	if resp.FlowID == nil || *resp.FlowID != flowID.String() {
		t.Fatalf("expected flow id %s in response", flowID)
	}
}

func TestAcceptFlow_CreatesPersonality(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	personalityID := uuid.New()

	var createdName string
	var createdPrompt string
	var acceptedFlowID uuid.UUID

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:              fid,
				Status:          "generated",
				CurrentStep:     4,
				Answers:         map[string]string{"name": "TestBot"},
				GeneratedPrompt: "You are TestBot, a helpful assistant.",
			}, nil
		},
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			createdName = p.Name
			createdPrompt = p.SystemPrompt
			return &models.Personality{ID: personalityID, Name: p.Name, SystemPrompt: p.SystemPrompt}, nil
		},
		getUserPreferencesFn: func(ctx context.Context, uid uuid.UUID) (*models.UserPreferences, error) {
			return &models.UserPreferences{
				ID:                   uuid.New(),
				UserID:               uid,
				DefaultModelID:       uuid.New(),
				DefaultPersonalityID: uuid.Nil,
				Theme:                "dark",
			}, nil
		},
		updateUserPrefsFn: func(ctx context.Context, uid uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error) {
			return &prefs, nil
		},
		acceptFlowFn: func(ctx context.Context, uid, fid, pid uuid.UUID) (*models.PersonalityGenFlow, error) {
			acceptedFlowID = fid
			return &models.PersonalityGenFlow{ID: fid, Status: "accepted"}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/accept", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	if createdName != "TestBot" {
		t.Fatalf("expected personality name 'TestBot', got %q", createdName)
	}
	if createdPrompt != "You are TestBot, a helpful assistant." {
		t.Fatalf("expected generated prompt, got %q", createdPrompt)
	}
	if acceptedFlowID != flowID {
		t.Fatalf("expected accepted flow id %s, got %s", flowID, acceptedFlowID)
	}
}

func TestAcceptFlow_NoGeneratedPrompt_Returns400(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:          fid,
				Status:      "in_progress",
				CurrentStep: 2,
				Answers:     map[string]string{"name": "Sparky"},
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/accept", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestAcceptFlow_InvalidOptionalBody_Returns400(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:              fid,
				Status:          "generated",
				CurrentStep:     4,
				Answers:         map[string]string{"name": "TestBot"},
				GeneratedPrompt: "You are TestBot, a helpful assistant.",
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/accept", strings.NewReader("{not-json"))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestAcceptFlow_AcceptLinkFails_Returns500(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	flowID := uuid.New()
	personalityID := uuid.New()

	store := &fakeStore{
		getFlowFn: func(ctx context.Context, uid, fid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return &models.PersonalityGenFlow{
				ID:              fid,
				Status:          "generated",
				CurrentStep:     4,
				Answers:         map[string]string{"name": "TestBot"},
				GeneratedPrompt: "You are TestBot, a helpful assistant.",
			}, nil
		},
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 1, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			return &models.Personality{ID: personalityID, Name: p.Name, SystemPrompt: p.SystemPrompt}, nil
		},
		acceptFlowFn: func(ctx context.Context, uid, fid, pid uuid.UUID) (*models.PersonalityGenFlow, error) {
			return nil, errors.New("accept failed")
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality/generate/"+flowID.String()+"/accept", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}
