package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type retryAgentStub struct {
	resp *models.ChatMessageResponse
	err  error
}

func (r *retryAgentStub) HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error) {
	return nil, nil
}

func (r *retryAgentStub) RetryUserChatMessage(ctx context.Context, chatID, messageID uuid.UUID) (*models.ChatMessageResponse, error) {
	return r.resp, r.err
}

type retryStoreStub struct {
	fakeStore
	msg       *models.ChatMessage
	activeJob *models.Job
}

func (r *retryStoreStub) GetChatMessage(ctx context.Context, userID, messageID uuid.UUID) (*models.ChatMessage, error) {
	if r.msg == nil {
		return nil, datastore.ErrChatMessageNotFound
	}
	return r.msg, nil
}

func (r *retryStoreStub) FindLatestActiveChatMessageJob(ctx context.Context, userID, userMessageID uuid.UUID) (*models.Job, error) {
	return r.activeJob, nil
}

func TestRetryChatMessage_Returns202(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()
	jobID := uuid.New()

	h := &Handler{
		ds: &retryStoreStub{
			msg: &models.ChatMessage{
				ID:     msgID,
				ChatID: chatID,
				Origin: models.MessageOriginUser,
			},
		},
		logger:       zap.NewNop(),
		messageAgent: &retryAgentStub{resp: &models.ChatMessageResponse{ID: msgID, JobID: jobID.String(), Type: agent.JobTypeChatMessage}},
	}

	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/chat-message/"+msgID.String()+"/retry", nil)
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String(), "messageId": msgID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	var body models.ChatMessageResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Equal(t, jobID.String(), body.JobID)
}

func TestGetActiveChatMessageJob_NoJob204(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()

	h := &Handler{
		ds: &retryStoreStub{
			msg: &models.ChatMessage{
				ID:     msgID,
				ChatID: chatID,
				Origin: models.MessageOriginUser,
			},
			activeJob: nil,
		},
		logger: zap.NewNop(),
	}

	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/chat/"+chatID.String()+"/chat-message/"+msgID.String()+"/active-job", nil)
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String(), "messageId": msgID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}
