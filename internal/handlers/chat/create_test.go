package chat

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

type createChatStore struct {
	fakeStore
	createChatFn func(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	modelByName  map[string]*models.Model
	modelErr     error
}

func (f *createChatStore) CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
	return f.createChatFn(ctx, userID, chat)
}

func (f *createChatStore) GetModelByName(ctx context.Context, name string) (*models.Model, error) {
	if f.modelErr != nil {
		return nil, f.modelErr
	}
	if f.modelByName == nil {
		return nil, datastore.ErrModelNotFound
	}
	m, ok := f.modelByName[name]
	if !ok || m == nil {
		return nil, datastore.ErrModelNotFound
	}
	return m, nil
}

func TestCreateChat_PassesPersonalityAndModelIDsToDatastore(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	modelID := uuid.New()

	var captured models.Chat
	store := &createChatStore{
		createChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			captured = chat
			return &models.Chat{ID: uuid.New(), Name: chat.Name, PersonalityID: chat.PersonalityID, ModelID: chat.ModelID}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	router.HandleFunc("/chat", h.CreateChat).Methods(http.MethodPost)

	body := `{"name":"Test","personality_id":"` + personalityID.String() + `","model_id":"` + modelID.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, personalityID, captured.PersonalityID)
	require.Equal(t, modelID, captured.ModelID)
}

func TestCreateChat_ReturnsNotFoundWhenPersonalityMissing(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &createChatStore{
		createChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			return nil, datastore.ErrPersonalityNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	router.HandleFunc("/chat", h.CreateChat).Methods(http.MethodPost)

	body := `{"name":"Test","personality_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	var resp models.ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "Personality not found", resp.Error)
}

func TestCreateChat_ReturnsNotFoundWhenModelMissing(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &createChatStore{
		createChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			return nil, datastore.ErrModelNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	router.HandleFunc("/chat", h.CreateChat).Methods(http.MethodPost)

	body := `{"name":"Test","model_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	var resp models.ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "Model not found", resp.Error)
}
