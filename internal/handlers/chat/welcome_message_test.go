package chat

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
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeWelcomeAgent struct {
	calls             int
	lastChatID        uuid.UUID
	lastPrompt        string
	lastModelOverride *uuid.UUID
	err               error
}

func (f *fakeWelcomeAgent) HandleWelcomeMessagePromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error) {
	f.calls++
	f.lastChatID = chatID
	f.lastPrompt = prompt
	f.lastModelOverride = modelOverrideID
	if f.err != nil {
		return nil, f.err
	}
	return &models.ChatMessageResponse{
		ID:    uuid.New(),
		JobID: "job-1",
		Type:  "agent_job_run",
	}, nil
}

func TestCreateWelcomeMessage_AcceptsAndReturnsJob(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	modelID := uuid.New()

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: id, UserID: uid, Name: "First"}, nil
		},
		isFirstChatFn: func(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
			return true, nil
		},
		countAllMessagesFn: func(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
			return 0, nil
		},
		getModelByNameFn: func(ctx context.Context, name string) (*models.Model, error) {
			return &models.Model{ID: modelID, Name: agent.FirstChatGreetingModelName}, nil
		},
	}
	welcomeAgent := &fakeWelcomeAgent{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.welcomeAgent = welcomeAgent

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, 1, welcomeAgent.calls)
	require.Equal(t, chatID, welcomeAgent.lastChatID)
	require.NotNil(t, welcomeAgent.lastModelOverride)
	require.Equal(t, modelID, *welcomeAgent.lastModelOverride)
	require.Contains(t, welcomeAgent.lastPrompt, "first assistant message")

	var payload models.ChatMessageResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
	require.Equal(t, "job-1", payload.JobID)
}

func TestCreateWelcomeMessage_NoOpWhenNotFirstChat(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: id, UserID: uid, Name: "Later"}, nil
		},
		isFirstChatFn: func(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
			return false, nil
		},
		countAllMessagesFn: func(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
			return 0, nil
		},
	}
	welcomeAgent := &fakeWelcomeAgent{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.welcomeAgent = welcomeAgent

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 0, welcomeAgent.calls)
}

func TestCreateWelcomeMessage_NoOpWhenUserAlreadyHasMessages(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: id, UserID: uid, Name: "First"}, nil
		},
		isFirstChatFn: func(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
			return true, nil
		},
		countAllMessagesFn: func(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
			return 1, nil
		},
	}
	welcomeAgent := &fakeWelcomeAgent{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.welcomeAgent = welcomeAgent

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 0, welcomeAgent.calls)
}

func TestCreateWelcomeMessage_FallbackWhenHaikuUnavailable(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: id, UserID: uid, Name: "First"}, nil
		},
		isFirstChatFn: func(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
			return true, nil
		},
		countAllMessagesFn: func(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
			return 0, nil
		},
		getModelByNameFn: func(ctx context.Context, name string) (*models.Model, error) {
			return nil, datastore.ErrModelNotFound
		},
	}
	welcomeAgent := &fakeWelcomeAgent{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.welcomeAgent = welcomeAgent

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, 1, welcomeAgent.calls)
	require.Nil(t, welcomeAgent.lastModelOverride)
}

func TestCreateWelcomeMessage_Errors(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()

	t.Run("unauthorized", func(t *testing.T) {
		h := NewHandler(&fakeStore{}, zap.NewNop(), nil, HandlerConfig{})
		h.welcomeAgent = &fakeWelcomeAgent{}
		router := mux.NewRouter()
		h.RegisterRoutes(router)

		req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid chat id", func(t *testing.T) {
		h := NewHandler(&fakeStore{}, zap.NewNop(), nil, HandlerConfig{})
		h.welcomeAgent = &fakeWelcomeAgent{}
		router := mux.NewRouter()
		h.RegisterRoutes(router)

		req := httptest.NewRequest(http.MethodPost, "/chat/not-a-uuid/welcome-message", nil)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
		req = mux.SetURLVars(req, map[string]string{"chatId": "not-a-uuid"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("chat not found", func(t *testing.T) {
		store := &fakeStore{
			getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
				return nil, datastore.ErrChatNotFound
			},
		}
		h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
		h.welcomeAgent = &fakeWelcomeAgent{}
		router := mux.NewRouter()
		h.RegisterRoutes(router)

		req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", nil)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
		req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("enqueue failure returns server error", func(t *testing.T) {
		store := &fakeStore{
			getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
				return &models.Chat{ID: id, UserID: uid, Name: "First"}, nil
			},
			isFirstChatFn: func(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
				return true, nil
			},
			countAllMessagesFn: func(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
				return 0, nil
			},
			getModelByNameFn: func(ctx context.Context, name string) (*models.Model, error) {
				return nil, datastore.ErrModelNotFound
			},
		}
		h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
		h.welcomeAgent = &fakeWelcomeAgent{err: errors.New("boom")}
		router := mux.NewRouter()
		h.RegisterRoutes(router)

		req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/welcome-message", strings.NewReader(""))
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
		req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
