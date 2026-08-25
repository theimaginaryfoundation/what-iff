package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type mockProvider struct {
	createChatMessageFn func(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error)
}

func (m *mockProvider) CreateWebhookToken(ctx context.Context, userID uuid.UUID, name string) (*models.WebhookToken, string, error) {
	return nil, "", errors.New("not implemented")
}

func (m *mockProvider) ListWebhookTokens(ctx context.Context, userID uuid.UUID) ([]*models.WebhookToken, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) RevokeWebhookToken(ctx context.Context, userID, tokenID uuid.UUID) error {
	return errors.New("not implemented")
}

func (m *mockProvider) CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error) {
	if m.createChatMessageFn != nil {
		return m.createChatMessageFn(ctx, userID, chatMessage)
	}
	return nil, errors.New("not implemented")
}

type mockAgent struct {
	handleUserMessageFn     func(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error)
	handleBackgroundAsyncFn func(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error)
	lastUserMessageInput    *models.ChatMessage
	lastBackgroundPrompt    string
	lastBackgroundChatID    uuid.UUID
}

func (m *mockAgent) HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error) {
	msg := request
	m.lastUserMessageInput = &msg
	if m.handleUserMessageFn != nil {
		return m.handleUserMessageFn(ctx, request)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAgent) HandleAgentJobPrompt(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID, ritualIDs []uuid.UUID, trackingJob *models.Job) (*models.ChatMessage, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAgent) HandleAgentJobPromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error) {
	m.lastBackgroundPrompt = prompt
	m.lastBackgroundChatID = chatID
	if m.handleBackgroundAsyncFn != nil {
		return m.handleBackgroundAsyncFn(ctx, chatID, prompt, modelOverrideID, personalityOverrideID)
	}
	return nil, errors.New("not implemented")
}

func newWebhookRequest(t *testing.T, body any, chatID uuid.UUID) *http.Request {
	t.Helper()
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
	return req
}

func TestSendChatMessage_Unauthorized(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockProvider{}, &mockAgent{}, zap.NewNop())
	rec := httptest.NewRecorder()
	req := newWebhookRequest(t, map[string]any{
		"mode":    "user",
		"message": "hello",
	}, uuid.New())

	h.SendChatMessage(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSendChatMessage_UserMode(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	userID := uuid.New()
	agent := &mockAgent{
		handleUserMessageFn: func(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error) {
			return &models.ChatMessageResponse{
				ID:    uuid.New(),
				JobID: "job-123",
				Type:  "chat_message",
			}, nil
		},
	}
	h := NewHandler(&mockProvider{}, agent, zap.NewNop())

	req := newWebhookRequest(t, map[string]any{
		"mode":    "user",
		"message": "hello webhook",
	}, chatID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	h.SendChatMessage(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.NotNil(t, agent.lastUserMessageInput)
	require.Equal(t, chatID, agent.lastUserMessageInput.ChatID)
	require.Equal(t, models.MessageOriginUser, agent.lastUserMessageInput.Origin)
}

func TestSendChatMessage_AssistantMode(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	userID := uuid.New()
	provider := &mockProvider{
		createChatMessageFn: func(ctx context.Context, gotUserID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error) {
			require.Equal(t, userID, gotUserID)
			require.Equal(t, chatID, chatMessage.ChatID)
			require.Equal(t, models.MessageOriginAssistant, chatMessage.Origin)
			require.Equal(t, "none", chatMessage.GenerationModel)
			require.Equal(t, "webhook", chatMessage.GenerationPersonality)
			return &models.ChatMessage{ID: uuid.New()}, nil
		},
	}
	h := NewHandler(provider, &mockAgent{}, zap.NewNop())

	req := newWebhookRequest(t, map[string]any{
		"mode":    "assistant",
		"message": "assistant payload",
	}, chatID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	h.SendChatMessage(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestSendChatMessage_BackgroundMode(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	userID := uuid.New()
	agent := &mockAgent{
		handleBackgroundAsyncFn: func(ctx context.Context, gotChatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error) {
			require.Equal(t, chatID, gotChatID)
			require.Equal(t, "run now", prompt)
			jobID := uuid.New()
			return &models.ChatMessageResponse{
				ID:    jobID,
				JobID: jobID.String(),
				Type:  "agent_job_run",
			}, nil
		},
	}
	h := NewHandler(&mockProvider{}, agent, zap.NewNop())

	req := newWebhookRequest(t, map[string]any{
		"mode":    "background",
		"message": "run now",
	}, chatID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	h.SendChatMessage(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
}

func TestSendChatMessage_CrossUserChatNotFound(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	userID := uuid.New()
	provider := &mockProvider{
		createChatMessageFn: func(ctx context.Context, _ uuid.UUID, _ models.ChatMessage) (*models.ChatMessage, error) {
			return nil, datastore.ErrChatNotFound
		},
	}
	h := NewHandler(provider, &mockAgent{}, zap.NewNop())

	req := newWebhookRequest(t, map[string]any{
		"mode":    "assistant",
		"message": "cross user attempt",
	}, chatID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()

	h.SendChatMessage(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
