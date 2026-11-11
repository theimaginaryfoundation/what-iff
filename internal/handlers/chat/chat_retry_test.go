package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type retryAgentStub struct {
	resp       *models.ChatMessageResponse
	err        error
	turnSource string
}

func (r *retryAgentStub) HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error) {
	return nil, nil
}

func (r *retryAgentStub) RetryUserChatMessage(ctx context.Context, chatID, messageID uuid.UUID) (*models.ChatMessageResponse, error) {
	r.turnSource = metering.TurnSourceFromContext(ctx)
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
	retryAgent := &retryAgentStub{resp: &models.ChatMessageResponse{ID: msgID, JobID: jobID.String(), Type: agent.JobTypeChatMessage}}

	h := &Handler{
		ds: &retryStoreStub{
			msg: &models.ChatMessage{
				ID:     msgID,
				ChatID: chatID,
				Origin: models.MessageOriginUser,
			},
		},
		logger:       zap.NewNop(),
		messageAgent: retryAgent,
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
	// A retry is sent from the app, so it is an app turn (ask_user in a relay thread, metering).
	require.Equal(t, metering.TurnSourceApp, retryAgent.turnSource)
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

type activeChatJobStoreStub struct {
	fakeStore
	job     *models.Job
	err     error
	gotChat uuid.UUID
	gotUser uuid.UUID
}

func (s *activeChatJobStoreStub) FindActiveTurnJobForChat(_ context.Context, userID, chatID uuid.UUID) (*models.Job, error) {
	s.gotUser, s.gotChat = userID, chatID
	return s.job, s.err
}

func serveActiveChatJob(t *testing.T, store *activeChatJobStoreStub, chatID string, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	h := &Handler{ds: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet, "/chat/"+chatID+"/active-job", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGetActiveChatJob(t *testing.T) {
	t.Parallel()

	t.Run("running turn returns job and its user message", func(t *testing.T) {
		t.Parallel()
		userID, chatID, jobID, msgID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		store := &activeChatJobStoreStub{job: &models.Job{ID: jobID, JobType: "chat_message", Status: models.JobStatusProcessing, Reference: msgID.String()}}

		rec := serveActiveChatJob(t, store, chatID.String(), userID)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, chatID, store.gotChat)
		require.Equal(t, userID, store.gotUser)
		require.JSONEq(t, `{"job_id":"`+jobID.String()+`","status":"processing","job_type":"chat_message","message_id":"`+msgID.String()+`"}`, rec.Body.String())
	})

	t.Run("scheduled run keyed by the chat answers no message and names its job", func(t *testing.T) {
		t.Parallel()
		userID, chatID, jobID, agentJobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		store := &activeChatJobStoreStub{job: &models.Job{
			ID: jobID, JobType: "agent_job_run", Status: models.JobStatusProcessing, Reference: chatID.String(),
			JobRun: &models.JobRun{AgentJobID: agentJobID, Title: "Morning tea", Prompt: "Suggest a tea."},
		}}

		rec := serveActiveChatJob(t, store, chatID.String(), userID)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.JSONEq(t, `{"job_id":"`+jobID.String()+`","status":"processing","job_type":"agent_job_run",`+
			`"job_run":{"agent_job_id":"`+agentJobID.String()+`","title":"Morning tea","prompt":"Suggest a tea."}}`, rec.Body.String())
	})

	// A webhook background run or the welcome is keyed by the chat too, but is no job's run.
	t.Run("chat-keyed run with no job has no job_run", func(t *testing.T) {
		t.Parallel()
		userID, chatID, jobID := uuid.New(), uuid.New(), uuid.New()
		store := &activeChatJobStoreStub{job: &models.Job{ID: jobID, JobType: "agent_job_run", Status: models.JobStatusProcessing, Reference: chatID.String()}}

		rec := serveActiveChatJob(t, store, chatID.String(), userID)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.JSONEq(t, `{"job_id":"`+jobID.String()+`","status":"processing","job_type":"agent_job_run"}`, rec.Body.String())
	})

	t.Run("nothing in flight is 204", func(t *testing.T) {
		t.Parallel()
		rec := serveActiveChatJob(t, &activeChatJobStoreStub{}, uuid.New().String(), uuid.New())
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Empty(t, rec.Body.String())
	})

	t.Run("invalid chat id is 400", func(t *testing.T) {
		t.Parallel()
		rec := serveActiveChatJob(t, &activeChatJobStoreStub{}, "not-a-uuid", uuid.New())
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("store failure is 500", func(t *testing.T) {
		t.Parallel()
		rec := serveActiveChatJob(t, &activeChatJobStoreStub{err: errors.New("db down")}, uuid.New().String(), uuid.New())
		require.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
