package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entjob "github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestAgentPrompt_AnswerOnceAndOnlyWhilePending(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)
	other := createMoodTestUser(t, ds)
	chatID, otherChat, jobID := uuid.New(), uuid.New(), uuid.New()

	newPrompt := func(expires time.Time) *models.AgentPrompt {
		p, err := ds.CreateAgentPrompt(ctx, &models.AgentPrompt{
			UserID: userID, ChatID: chatID, JobID: jobID, Source: models.AgentPromptSourceAgent, ToolCallID: "call_1",
			Payload:   models.AgentPromptPayload{Kind: models.AgentPromptKindApproval, Title: "Send it"},
			ExpiresAt: expires,
		})
		require.NoError(t, err)
		return p
	}
	accept := models.AgentPromptResponse{Action: models.AgentPromptAccept, Note: "go"}

	p := newPrompt(time.Now().Add(time.Minute))
	require.Equal(t, models.AgentPromptPending, p.Status)
	require.Equal(t, "Send it", p.Payload.Title)

	_, err := ds.AnswerAgentPrompt(ctx, other, chatID, p.ID, accept)
	require.ErrorIs(t, err, ErrAgentPromptNotFound, "another account cannot answer")
	_, err = ds.AnswerAgentPrompt(ctx, userID, otherChat, p.ID, accept)
	require.ErrorIs(t, err, ErrAgentPromptNotFound, "nor from another chat")

	answered, err := ds.AnswerAgentPrompt(ctx, userID, chatID, p.ID, accept)
	require.NoError(t, err)
	require.Equal(t, models.AgentPromptAnswered, answered.Status)
	require.Equal(t, "go", answered.Response.Note)
	require.NotNil(t, answered.AnsweredAt)

	_, err = ds.AnswerAgentPrompt(ctx, userID, chatID, p.ID, models.AgentPromptResponse{Action: models.AgentPromptDecline})
	require.ErrorIs(t, err, ErrAgentPromptClosed, "a second answer is refused")
	closed, err := ds.CloseAgentPrompt(ctx, userID, p.ID, models.AgentPromptExpired)
	require.NoError(t, err)
	require.False(t, closed, "an answered prompt cannot be closed")

	expired := newPrompt(time.Now().Add(-time.Second))
	_, err = ds.AnswerAgentPrompt(ctx, userID, chatID, expired.ID, accept)
	require.ErrorIs(t, err, ErrAgentPromptClosed, "past its expiry it takes no answer")

	live := newPrompt(time.Now().Add(time.Minute))
	closed, err = ds.CloseAgentPrompt(ctx, userID, live.ID, models.AgentPromptCancelled)
	require.NoError(t, err)
	require.True(t, closed)
	got, err := ds.GetAgentPrompt(ctx, userID, live.ID)
	require.NoError(t, err)
	require.Equal(t, models.AgentPromptCancelled, got.Status)
	_, err = ds.AnswerAgentPrompt(ctx, userID, chatID, live.ID, accept)
	require.ErrorIs(t, err, ErrAgentPromptClosed, "a cancelled prompt takes no answer")
}

func TestClaimSteerMessages(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)
	chat, err := ds.dbClient.Chat.Create().SetName("c").SetOwnerID(userID).Save(ctx)
	require.NoError(t, err)
	otherChat, err := ds.dbClient.Chat.Create().SetName("o").SetOwnerID(userID).Save(ctx)
	require.NoError(t, err)

	message := func(chatID uuid.UUID, text string) *models.ChatMessage {
		m, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chatID, Message: text, Origin: models.MessageOriginUser})
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond) // distinct created_at for ordering
		return m
	}
	job := func(m *models.ChatMessage, steer bool, status models.JobStatus) *models.Job {
		j, err := ds.CreateJob(ctx, userID, models.Job{JobType: "chat_message", Reference: m.ID.String(), Status: status, Steer: steer})
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
		return j
	}

	earlySteer := job(message(chat.ID, "queued before the turn"), true, models.JobStatusPending)
	running := job(message(chat.ID, "do the research"), false, models.JobStatusProcessing)
	first := job(message(chat.ID, "also check 2025"), true, models.JobStatusProcessing)
	plain := job(message(chat.ID, "a normal queued turn"), false, models.JobStatusPending)
	second := job(message(chat.ID, "and keep it short"), true, models.JobStatusPending)
	elsewhere := job(message(otherChat.ID, "other thread"), true, models.JobStatusPending)

	claimed, err := ds.ClaimSteerMessages(ctx, userID, chat.ID, running.ID, running.CreatedAt)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, "also check 2025", claimed[0].Message)
	require.Equal(t, "and keep it short", claimed[1].Message)

	status := func(j *models.Job) (models.JobStatus, string) {
		row, err := ds.dbClient.Job.Query().Where(entjob.ID(j.ID)).Only(ctx)
		require.NoError(t, err)
		return models.JobStatus(row.Status), row.ErrorCode
	}
	for _, j := range []*models.Job{first, second} {
		s, code := status(j)
		require.Equal(t, models.JobStatusCancelled, s)
		require.Equal(t, SteerJobErrorCode, code)
	}
	for _, j := range []*models.Job{earlySteer, plain, elsewhere} {
		s, _ := status(j)
		require.NotEqual(t, models.JobStatusCancelled, s, "not a follow-up to this turn")
	}

	again, err := ds.ClaimSteerMessages(ctx, userID, chat.ID, running.ID, running.CreatedAt)
	require.NoError(t, err)
	require.Empty(t, again, "each follow-up is claimed once")
}
