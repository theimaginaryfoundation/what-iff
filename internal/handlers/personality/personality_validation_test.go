package personality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestCreatePersonality_SystemPromptTooLong(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			t.Fatalf("CreatePersonality should not be called for oversized prompt")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	payload := map[string]any{
		"name":          "TooLong",
		"system_prompt": strings.Repeat("a", handlerutils.TextLimitHardMax+1),
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp models.ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, models.ErrCodeSystemPromptTooLong, resp.Code)
}

func TestListPersonalities_PassesPersonalityIDsFilter(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	firstID := uuid.New()
	secondID := uuid.New()

	var gotFilters models.PersonalityFilters
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			require.Equal(t, userID, uid)
			require.Equal(t, 1, pageNum)
			require.Equal(t, 10, pageSize)
			gotFilters = filters
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/personality?personality_ids="+firstID.String()+","+secondID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []uuid.UUID{firstID, secondID}, gotFilters.IDs)
}

func TestListPersonalities_InvalidPersonalityIDsReturnsBadRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			t.Fatalf("ListPersonalities should not be called for malformed personality_ids")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/personality?personality_ids=not-a-uuid", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdatePersonality_SystemPromptTooLong(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	store := &fakeStore{
		updatePersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			t.Fatalf("UpdatePersonality should not be called for oversized prompt")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	payload := map[string]any{
		"name":          "TooLongUpdate",
		"system_prompt": strings.Repeat("b", handlerutils.TextLimitHardMax+1),
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String(), strings.NewReader(string(body)))
	req = mux.SetURLVars(req, map[string]string{"id": personalityID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp models.ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, models.ErrCodeSystemPromptTooLong, resp.Code)
}

func TestCreatePersonality_InvalidAccentColorReturnsBadRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			t.Fatalf("CreatePersonality should not be called for invalid accent color")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"name":"Vera","system_prompt":"sp","accent_color":"purple"}`
	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdatePersonality_InvalidThumbnailCircleReturnsBadRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	store := &fakeStore{
		updatePersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			t.Fatalf("UpdatePersonality should not be called for invalid thumbnail circle")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"name":"Vera","system_prompt":"sp","thumbnail_circle":{"cx":1.1,"cy":0.9,"r":0.2}}`
	req := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String(), strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": personalityID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
