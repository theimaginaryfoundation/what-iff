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
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestCreatePersonality_PassesCoverImageIDToDatastore(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	coverID := uuid.New()
	newPersonalityID := uuid.New()

	var capturedCoverID *uuid.UUID
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{map[string]any{}}, TotalCount: 5, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			capturedCoverID = p.CoverImageID
			return &models.Personality{ID: newPersonalityID, Name: p.Name, SystemPrompt: p.SystemPrompt, CoverImageID: p.CoverImageID}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"name":"Cover","system_prompt":"sp","cover_image_id":"` + coverID.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NotNil(t, capturedCoverID)
	require.Equal(t, coverID, *capturedCoverID)
}

func TestCreatePersonality_PassesAccentAndThumbnailCircleToDatastore(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	newPersonalityID := uuid.New()
	expectedAccent := "#C2572A"

	var capturedAccent *string
	var capturedCircle *models.PersonalityThumbnailCircle
	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{map[string]any{}}, TotalCount: 5, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			capturedAccent = p.AccentColor
			capturedCircle = p.ThumbnailCircle
			return &models.Personality{ID: newPersonalityID, Name: p.Name, SystemPrompt: p.SystemPrompt}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"name":"Vera","system_prompt":"sp","accent_color":"#C2572A","thumbnail_circle":{"cx":0.5,"cy":0.42,"r":0.34}}`
	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NotNil(t, capturedAccent)
	require.Equal(t, expectedAccent, *capturedAccent)
	require.NotNil(t, capturedCircle)
	require.InEpsilon(t, 0.5, capturedCircle.CX, 0.0001)
	require.InEpsilon(t, 0.42, capturedCircle.CY, 0.0001)
	require.InEpsilon(t, 0.34, capturedCircle.R, 0.0001)
}

func TestCreatePersonality_ReturnsNotFoundWhenCoverImageMissing(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	coverID := uuid.New()

	store := &fakeStore{
		listPersonalitiesFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{map[string]any{}}, TotalCount: 5, Page: 1}, nil
		},
		createPersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			return nil, datastore.ErrFileAttachmentNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"name":"Cover","system_prompt":"sp","cover_image_id":"` + coverID.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/personality", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	var resp models.ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "Cover image not found", resp.Error)
}

func TestUpdatePersonality_ReturnsNotFoundWhenCoverImageMissing(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	coverID := uuid.New()

	store := &fakeStore{
		updatePersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
			return nil, datastore.ErrFileAttachmentNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"name":"Cover","system_prompt":"sp","cover_image_id":"` + coverID.String() + `"}`
	req := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String(), strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	var resp models.ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "Cover image not found", resp.Error)
}
