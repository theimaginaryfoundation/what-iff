package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Datastore half of the issue #254 turn serialization: the per-chat turn-job listing the turn gate
// polls, the heartbeat, and Stop's coverage of agent_job_run turns.

func TestTurnJobsForChat_GateListingAndStop(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	chatA, chatB := uuid.New(), uuid.New()
	createTestChat(t, ds, chatA, userID)
	createTestChat(t, ds, chatB, userID)
	turnA1 := createJobTestUserMessage(t, ds, chatA)
	turnA2 := createJobTestUserMessage(t, ds, chatA)
	turnB := createJobTestUserMessage(t, ds, chatB)

	createJob := func(jobType, reference string, status models.JobStatus) *models.Job {
		t.Helper()
		j := createActiveChatMessageJob(t, ds, userID, reference, status)
		if jobType != "chat_message" {
			_, err := ds.dbClient.Job.UpdateOneID(j.ID).SetJobType(jobType).Save(ctx)
			require.NoError(t, err)
			j.JobType = jobType
		}
		return j
	}

	createJob("chat_message", turnA1.String(), models.JobStatusComplete)
	createJob("chat_message", turnA1.String(), models.JobStatusFailed)
	createJob("agent_job_run", chatA.String(), models.JobStatusCancelled)
	replied := createJob("chat_message", turnA1.String(), models.JobStatusInferenceComplete)
	webhook := createJob("agent_job_run", chatA.String(), models.JobStatusProcessing)
	syncRun := createJob("agent_job_run", turnA2.String(), models.JobStatusPending)
	self := createJob("chat_message", turnA2.String(), models.JobStatusPending)
	// Not turns in chatA: another thread's turn, a non-turn job keyed on the chat id, and a job
	// whose reference is neither a chat nor a message.
	otherThread := createJob("chat_message", turnB.String(), models.JobStatusProcessing)
	createJob("agent_job_run", chatB.String(), models.JobStatusProcessing)
	createJob("thread_rehydration", chatA.String(), models.JobStatusProcessing)
	createJob("chat_message", "not-a-uuid", models.JobStatusProcessing)

	// The gate's listing: turns in the chat still before their reply, oldest first, whether keyed
	// by chat or by message, leaving out the asking job.
	got, err := ds.ListPendingTurnJobsForChat(ctx, userID, chatA, self.ID)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(got))
	for _, j := range got {
		ids = append(ids, j.ID)
		require.Equal(t, userID, j.UserID)
		require.False(t, j.CreatedAt.IsZero())
		require.False(t, j.UpdatedAt.IsZero())
	}
	require.Equal(t, []uuid.UUID{webhook.ID, syncRun.ID}, ids,
		"pending/processing turns in the chat; a turn past inference_complete no longer blocks")

	// Another user never sees them.
	stranger := createJobTestUser(t, ds)
	got, err = ds.ListPendingTurnJobsForChat(ctx, stranger, chatA, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, got)

	// Heartbeat: TouchJob moves updated_at forward, and only for the owner.
	time.Sleep(5 * time.Millisecond)
	stale, err := ds.dbClient.Job.Get(ctx, webhook.ID)
	require.NoError(t, err)
	require.NoError(t, ds.TouchJob(ctx, stranger, webhook.ID))
	unchanged, err := ds.dbClient.Job.Get(ctx, webhook.ID)
	require.NoError(t, err)
	require.True(t, unchanged.UpdatedAt.Equal(stale.UpdatedAt))
	require.NoError(t, ds.TouchJob(ctx, userID, webhook.ID))
	touched, err := ds.dbClient.Job.Get(ctx, webhook.ID)
	require.NoError(t, err)
	require.True(t, touched.UpdatedAt.After(stale.UpdatedAt))

	// Stop covers agent_job_run turns too: listed (newest first, every non-terminal status),
	// resolvable to their chat by chat or message reference, and cancellable.
	stopIDs, err := ds.ListActiveChatJobIDsForChat(ctx, userID, chatA)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{self.ID, syncRun.ID, webhook.ID, replied.ID}, stopIDs)
	for _, id := range []uuid.UUID{webhook.ID, syncRun.ID} {
		chatID, err := ds.ChatIDForChatJob(ctx, userID, id)
		require.NoError(t, err)
		require.Equal(t, chatA, chatID)
	}
	_, err = ds.ChatIDForChatJob(ctx, stranger, webhook.ID)
	require.ErrorIs(t, err, ErrJobNotFound)
	changed, err := ds.MarkChatJobCancelled(ctx, userID, webhook.ID)
	require.NoError(t, err)
	require.True(t, changed)
	st, err := ds.JobStatus(ctx, userID, webhook.ID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusCancelled, st)
	st, err = ds.JobStatus(ctx, userID, otherThread.ID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusProcessing, st)
}

// FinishTurnJobIfActive decides from the row and only writes a job that is still non-terminal, so a
// worker finishing its own abandoned job never overwrites a status another instance wrote.
func TestFinishTurnJobIfActive(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createJobTestUser(t, ds)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	msgID := createJobTestUserMessage(t, ds, chatID)

	for name, tc := range map[string]struct {
		status      models.JobStatus
		rowResult   bool
		replied     bool
		wantWritten models.JobStatus
		wantStatus  models.JobStatus
	}{
		"no reply fails":              {models.JobStatusProcessing, false, false, models.JobStatusFailed, models.JobStatusFailed},
		"worker saw its reply":        {models.JobStatusProcessing, false, true, models.JobStatusComplete, models.JobStatusComplete},
		"row has a result":            {models.JobStatusProcessing, true, false, models.JobStatusComplete, models.JobStatusComplete},
		"row is past its reply":       {models.JobStatusExpressionComplete, false, false, models.JobStatusComplete, models.JobStatusComplete},
		"cancelled elsewhere is kept": {models.JobStatusCancelled, false, true, "", models.JobStatusCancelled},
		"failed elsewhere is kept":    {models.JobStatusFailed, true, true, "", models.JobStatusFailed},
		"completed elsewhere is kept": {models.JobStatusComplete, false, false, "", models.JobStatusComplete},
	} {
		t.Run(name, func(t *testing.T) {
			j := createActiveChatMessageJob(t, ds, userID, msgID.String(), tc.status)
			if tc.rowResult {
				_, err := ds.dbClient.Job.UpdateOneID(j.ID).SetResultID(uuid.New()).Save(ctx)
				require.NoError(t, err)
			}
			written, err := ds.FinishTurnJobIfActive(ctx, userID, j.ID, tc.replied, "turn ended without a final status")
			require.NoError(t, err)
			require.Equal(t, tc.wantWritten, written)
			got, err := ds.GetJob(ctx, userID, j.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, got.Status)
			if tc.wantWritten == models.JobStatusFailed {
				require.Equal(t, "turn ended without a final status", got.Error)
			}
		})
	}

	// Another user's job is never touched.
	j := createActiveChatMessageJob(t, ds, userID, msgID.String(), models.JobStatusProcessing)
	written, err := ds.FinishTurnJobIfActive(ctx, uuid.New(), j.ID, false, "x")
	require.NoError(t, err)
	require.Empty(t, written)
	st, err := ds.JobStatus(ctx, userID, j.ID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusProcessing, st)
}
