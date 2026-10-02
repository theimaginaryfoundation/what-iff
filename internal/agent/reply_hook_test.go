package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func captureReplyHook(t *testing.T) <-chan replyhook.Event {
	t.Helper()
	got := make(chan replyhook.Event, 1)
	unregister := replyhook.Register(func(_ context.Context, ev replyhook.Event) { got <- ev })
	t.Cleanup(unregister)
	return got
}

func receiveReplyEvent(t *testing.T, got <-chan replyhook.Event) replyhook.Event {
	t.Helper()
	select {
	case ev := <-got:
		return ev
	case <-time.After(time.Second):
		t.Fatal("reply hook was not called")
		return replyhook.Event{}
	}
}

func TestFireReplyHookCarriesTheReplyTriggerAndJob(t *testing.T) {
	got := captureReplyHook(t)
	a := &Agent{lifecycleCtx: context.Background(), logger: zap.NewNop()}

	userID := uuid.New()
	reply := &models.ChatMessage{ID: uuid.New(), ChatID: uuid.New()}
	trigger := &models.ChatMessage{ID: uuid.New(), ChatID: reply.ChatID}
	job := &models.Job{ID: uuid.New(), JobType: JobTypeChatMessage}

	a.fireReplyHook(userID, reply, trigger, job, userTurnCallPath(job))

	ev := receiveReplyEvent(t, got)
	assert.Equal(t, userID, ev.UserID)
	assert.Equal(t, reply.ChatID, ev.ChatID)
	assert.Equal(t, reply.ID, ev.MessageID)
	require.NotNil(t, ev.TriggerMessageID)
	assert.Equal(t, trigger.ID, *ev.TriggerMessageID)
	require.NotNil(t, ev.JobID)
	assert.Equal(t, job.ID, *ev.JobID)
	assert.Equal(t, telemetry.CallPathUserChat, ev.CallPath)
}

func TestFireReplyHookForAnEphemeralPromptHasNoTrigger(t *testing.T) {
	got := captureReplyHook(t)
	a := &Agent{lifecycleCtx: context.Background(), logger: zap.NewNop()}

	reply := &models.ChatMessage{ID: uuid.New(), ChatID: uuid.New()}
	a.fireReplyHook(uuid.New(), reply, nil, nil, telemetry.CallPathAgentJob)

	ev := receiveReplyEvent(t, got)
	assert.Nil(t, ev.TriggerMessageID)
	assert.Nil(t, ev.JobID)
	assert.Equal(t, telemetry.CallPathAgentJob, ev.CallPath)
}

func TestFireReplyHookSkipsAMissingReply(t *testing.T) {
	got := captureReplyHook(t)
	a := &Agent{lifecycleCtx: context.Background(), logger: zap.NewNop()}

	a.fireReplyHook(uuid.New(), nil, nil, nil, telemetry.CallPathUserChat)

	select {
	case <-got:
		t.Fatal("hook fired without a reply")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUserTurnCallPathLabelsAgentJobRunsAsJobs(t *testing.T) {
	assert.Equal(t, telemetry.CallPathUserChat, userTurnCallPath(nil))
	assert.Equal(t, telemetry.CallPathUserChat, userTurnCallPath(&models.Job{JobType: JobTypeChatMessage}))
	assert.Equal(t, telemetry.CallPathAgentJob, userTurnCallPath(&models.Job{JobType: JobTypeAgentJobRun}))
}
