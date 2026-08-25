package personality

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeStore struct {
	listPersonalitiesFn                  func(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error)
	createPersonalityFn                  func(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	updatePersonalityFn                  func(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	listExpressionsFn                    func(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityExpression, error)
	upsertExpressionFn                   func(ctx context.Context, userID, personalityID uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error)
	deleteExpressionFn                   func(ctx context.Context, userID, personalityID uuid.UUID, key string) error
	getUserPreferencesFn                 func(ctx context.Context, userID uuid.UUID) (*models.UserPreferences, error)
	updateUserPrefsFn                    func(ctx context.Context, userID uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error)
	getOrCreateActiveFlowFn              func(ctx context.Context, userID uuid.UUID) (*models.PersonalityGenFlow, error)
	getFlowFn                            func(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error)
	updateFlowFn                         func(ctx context.Context, userID uuid.UUID, flowID uuid.UUID, req models.UpdateFlowRequest) (*models.PersonalityGenFlow, error)
	resetFlowFn                          func(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error)
	setFlowGeneratedFn                   func(ctx context.Context, userID, flowID uuid.UUID, prompt, aboutMe string, names []string) (*models.PersonalityGenFlow, error)
	acceptFlowFn                         func(ctx context.Context, userID, flowID, personalityID uuid.UUID) (*models.PersonalityGenFlow, error)
	findActivePersonalityMediaJobFn      func(ctx context.Context, userID uuid.UUID) (*models.Job, error)
	findActivePersonalityGenerationJobFn func(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error)
}

func (f *fakeStore) CreatePersonality(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error) {
	return f.createPersonalityFn(ctx, userID, personality)
}
func (f *fakeStore) ListPersonalities(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
	return f.listPersonalitiesFn(ctx, userID, pageNum, pageSize, filters)
}
func (f *fakeStore) GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) UpdatePersonality(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error) {
	if f.updatePersonalityFn != nil {
		return f.updatePersonalityFn(ctx, userID, personality)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeletePersonality(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) ListPersonalityExpressions(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityExpression, error) {
	if f.listExpressionsFn != nil {
		return f.listExpressionsFn(ctx, userID, personalityID)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) UpsertPersonalityExpression(ctx context.Context, userID, personalityID uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error) {
	if f.upsertExpressionFn != nil {
		return f.upsertExpressionFn(ctx, userID, personalityID, key, req)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeletePersonalityExpression(ctx context.Context, userID, personalityID uuid.UUID, key string) error {
	if f.deleteExpressionFn != nil {
		return f.deleteExpressionFn(ctx, userID, personalityID, key)
	}
	return errors.New("not implemented")
}
func (f *fakeStore) CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error {
	return errors.New("not implemented")
}
func (f *fakeStore) GetUserPreferences(ctx context.Context, userID uuid.UUID) (*models.UserPreferences, error) {
	return f.getUserPreferencesFn(ctx, userID)
}
func (f *fakeStore) UpdateUserPreferences(ctx context.Context, userID uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error) {
	return f.updateUserPrefsFn(ctx, userID, prefs)
}
func (f *fakeStore) GetOrCreateActiveFlow(ctx context.Context, userID uuid.UUID) (*models.PersonalityGenFlow, error) {
	if f.getOrCreateActiveFlowFn != nil {
		return f.getOrCreateActiveFlowFn(ctx, userID)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetFlow(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error) {
	if f.getFlowFn != nil {
		return f.getFlowFn(ctx, userID, flowID)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) UpdateFlow(ctx context.Context, userID uuid.UUID, flowID uuid.UUID, req models.UpdateFlowRequest) (*models.PersonalityGenFlow, error) {
	if f.updateFlowFn != nil {
		return f.updateFlowFn(ctx, userID, flowID, req)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ResetFlow(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error) {
	if f.resetFlowFn != nil {
		return f.resetFlowFn(ctx, userID, flowID)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) SetFlowGenerated(ctx context.Context, userID, flowID uuid.UUID, prompt, aboutMe string, names []string) (*models.PersonalityGenFlow, error) {
	if f.setFlowGeneratedFn != nil {
		return f.setFlowGeneratedFn(ctx, userID, flowID, prompt, aboutMe, names)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) AcceptFlow(ctx context.Context, userID, flowID, personalityID uuid.UUID) (*models.PersonalityGenFlow, error) {
	if f.acceptFlowFn != nil {
		return f.acceptFlowFn(ctx, userID, flowID, personalityID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeStore) FindActivePersonalityMediaJob(ctx context.Context, userID uuid.UUID) (*models.Job, error) {
	if f.findActivePersonalityMediaJobFn != nil {
		return f.findActivePersonalityMediaJobFn(ctx, userID)
	}
	return nil, nil
}

func (f *fakeStore) FindActivePersonalityGenerationJob(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error) {
	if f.findActivePersonalityGenerationJobFn != nil {
		return f.findActivePersonalityGenerationJobFn(ctx, userID, flowID)
	}
	return nil, nil
}

func TestCreatePersonality_FirstPersonality_SetsDefaultPreference(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	newPersonalityID := uuid.New()

	var updatedDefault uuid.UUID
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			return &models.Personality{ID: newPersonalityID, Name: p.Name, SystemPrompt: p.SystemPrompt}, nil
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
			updatedDefault = prefs.DefaultPersonalityID
			return &prefs, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(`{"name":"P1","system_prompt":"do stuff"}`))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	if updatedDefault != newPersonalityID {
		t.Fatalf("expected default_personality_id to be set to %s, got %s", newPersonalityID, updatedDefault)
	}

	var resp models.Personality
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != newPersonalityID {
		t.Fatalf("expected created personality id %s, got %s", newPersonalityID, resp.ID)
	}
}

// Creating the first personality reads the user's preferences, changes one field, and
// writes the whole struct back. Any field this handler does not know about rides along in
// that round trip, so a field dropped here is silently destroyed by an unrelated feature.
// Favorites are the current example; the assertion is really about the pattern.
func TestCreatePersonality_FirstPersonality_PreservesUnrelatedPreferences(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	newPersonalityID := uuid.New()

	// Every field is populated so the equality check below has something to catch. A new
	// preference field added without extending this fixture still fails the comparison the
	// moment the handler drops it, which is the point.
	stored := models.UserPreferences{
		ID:                   uuid.New(),
		UserID:               userID,
		DefaultModelID:       uuid.New(),
		DefaultPersonalityID: uuid.Nil,
		Theme:                "dark",
		LastSeenAnnouncement: "2026-08-release",
		FavoriteModelIDs:     []string{"model-a", "model-b"},
	}

	var written models.UserPreferences
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			return &models.Personality{ID: newPersonalityID, Name: p.Name, SystemPrompt: p.SystemPrompt}, nil
		},
		getUserPreferencesFn: func(ctx context.Context, uid uuid.UUID) (*models.UserPreferences, error) {
			prefs := stored
			return &prefs, nil
		},
		updateUserPrefsFn: func(ctx context.Context, uid uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error) {
			written = prefs
			return &prefs, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(`{"name":"P1","system_prompt":"do stuff"}`))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	// Compare the whole struct rather than naming fields: the hazard is a field being
	// dropped, and listing them here would miss exactly the ones added after this was
	// written. Only default_personality_id may differ.
	want := stored
	want.DefaultPersonalityID = newPersonalityID

	if !reflect.DeepEqual(written, want) {
		t.Fatalf("preferences changed beyond the default personality during read-modify-write:\n got: %+v\nwant: %+v",
			written, want)
	}
}

func TestCreatePersonality_NotFirstPersonality_DoesNotUpdateDefaultPreference(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	newPersonalityID := uuid.New()

	updated := false
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{map[string]any{}}, TotalCount: 2, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			return &models.Personality{ID: newPersonalityID, Name: p.Name, SystemPrompt: p.SystemPrompt}, nil
		},
		getUserPreferencesFn: func(ctx context.Context, uid uuid.UUID) (*models.UserPreferences, error) {
			t.Fatalf("GetUserPreferences should not be called when not first personality")
			return nil, nil
		},
		updateUserPrefsFn: func(ctx context.Context, uid uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error) {
			updated = true
			return &prefs, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(`{"name":"P2","system_prompt":"do stuff"}`))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	if updated {
		t.Fatalf("did not expect UpdateUserPreferences to be called")
	}
}
