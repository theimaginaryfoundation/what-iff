package personality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestUpsertExpression_PassesParsedRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	imageID := uuid.New()
	imageURL := "/api/image-gallery/" + imageID.String() + "?size=full"
	label := "Happy"
	now := time.Now().UTC()

	var gotKey string
	var gotReq models.UpdatePersonalityExpressionRequest
	store := &fakeStore{
		upsertExpressionFn: func(ctx context.Context, uid, pid uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error) {
			require.Equal(t, userID, uid)
			require.Equal(t, personalityID, pid)
			gotKey = key
			gotReq = req
			return &models.PersonalityExpression{
				ExpressionKey: key,
				Label:         &label,
				ImageID:       &imageID,
				ImageURL:      &imageURL,
				CreatedAt:     now,
				UpdatedAt:     now,
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String()+"/expressions/happy", strings.NewReader(`{"image_id":"`+imageID.String()+`","label":"Happy"}`))
	req = mux.SetURLVars(req, map[string]string{"id": personalityID.String(), "expression_key": "happy"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "happy", gotKey)
	require.True(t, gotReq.ImageSet)
	require.Equal(t, imageID, *gotReq.ImageID)
	require.True(t, gotReq.LabelSet)
	require.Equal(t, "Happy", *gotReq.Label)

	var resp models.PersonalityExpression
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, imageURL, *resp.ImageURL)
}

func TestUpsertExpression_AllowsExplicitNulls(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()

	var gotReq models.UpdatePersonalityExpressionRequest
	store := &fakeStore{
		upsertExpressionFn: func(ctx context.Context, uid, pid uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error) {
			gotReq = req
			return &models.PersonalityExpression{ExpressionKey: key, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String()+"/expressions/happy", strings.NewReader(`{"image_id":null,"label":null}`))
	req = mux.SetURLVars(req, map[string]string{"id": personalityID.String(), "expression_key": "happy"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, gotReq.ImageSet)
	require.Nil(t, gotReq.ImageID)
	require.True(t, gotReq.LabelSet)
	require.Nil(t, gotReq.Label)
}

func TestUpsertExpression_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		body string
	}{
		{name: "invalid key", key: "Happy!", body: `{"label":"Happy"}`},
		{name: "invalid image id", key: "happy", body: `{"image_id":"not-a-uuid"}`},
		{name: "empty body", key: "happy", body: `{}`},
		{name: "label too long", key: "happy", body: `{"label":"` + strings.Repeat("x", maxExpressionLabelLength+1) + `"}`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			personalityID := uuid.New()
			store := &fakeStore{
				upsertExpressionFn: func(ctx context.Context, uid, pid uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error) {
					t.Fatalf("UpsertPersonalityExpression should not be called")
					return nil, nil
				},
			}

			h := NewHandler(store, zap.NewNop(), nil)
			router := mux.NewRouter()
			h.RegisterRoutes(router)

			req := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String()+"/expressions/"+tc.key, strings.NewReader(tc.body))
			req = mux.SetURLVars(req, map[string]string{"id": personalityID.String(), "expression_key": tc.key})
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestExpressionHandlers_MapNotFoundErrors(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	store := &fakeStore{
		listExpressionsFn: func(ctx context.Context, uid, pid uuid.UUID) ([]models.PersonalityExpression, error) {
			return nil, datastore.ErrPersonalityNotFound
		},
		upsertExpressionFn: func(ctx context.Context, uid, pid uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error) {
			return nil, datastore.ErrFileAttachmentNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	listReq := httptest.NewRequest(http.MethodGet, "/personality/"+personalityID.String()+"/expressions", nil)
	listReq = mux.SetURLVars(listReq, map[string]string{"id": personalityID.String()})
	listReq = listReq.WithContext(context.WithValue(listReq.Context(), middleware.UserIDKey, userID))
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	require.Equal(t, http.StatusNotFound, listW.Code)

	upsertReq := httptest.NewRequest(http.MethodPut, "/personality/"+personalityID.String()+"/expressions/happy", strings.NewReader(`{"label":"Happy"}`))
	upsertReq = mux.SetURLVars(upsertReq, map[string]string{"id": personalityID.String(), "expression_key": "happy"})
	upsertReq = upsertReq.WithContext(context.WithValue(upsertReq.Context(), middleware.UserIDKey, userID))
	upsertW := httptest.NewRecorder()
	router.ServeHTTP(upsertW, upsertReq)
	require.Equal(t, http.StatusNotFound, upsertW.Code)
}

func TestDeleteExpression_ReturnsNoContent(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()

	var gotKey string
	store := &fakeStore{
		deleteExpressionFn: func(ctx context.Context, uid, pid uuid.UUID, key string) error {
			require.Equal(t, userID, uid)
			require.Equal(t, personalityID, pid)
			gotKey = key
			return nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodDelete, "/personality/"+personalityID.String()+"/expressions/happy", nil)
	req = mux.SetURLVars(req, map[string]string{"id": personalityID.String(), "expression_key": "happy"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "happy", gotKey)
}
