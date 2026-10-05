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
	checkpointing := createJob("chat_message", turnA1.String(), models.JobStatusExpressionComplete)
	// Abandoned by a crash mid-checkpoint: unfinished, but no heartbeat for an hour.
	abandoned := createJob("chat_message", turnA1.String(), models.JobStatusExpressionComplete)
	_, err := ds.dbClient.Job.UpdateOneID(abandoned.ID).SetUpdatedAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	webhook := createJob("agent_job_run", chatA.String(), models.JobStatusProcessing)
	syncRun := createJob("agent_job_run", turnA2.String(), models.JobStatusPending)
	self := createJob("chat_message", turnA2.String(), models.JobStatusPending)
	// Not turns in chatA: another thread's turn, a non-turn job keyed on the chat id, and a job
	// whose reference is neither a chat nor a message.
	otherThread := createJob("chat_message", turnB.String(), models.JobStatusProcessing)
	createJob("agent_job_run", chatB.String(), models.JobStatusProcessing)
	createJob("thread_rehydration", chatA.String(), models.JobStatusProcessing)
	createJob("chat_message", "not-a-uuid", models.JobStatusProcessing)

	// The gate's listing: unfinished turns in the chat (before or after their reply), oldest first,
	// whether keyed by chat or by message, leaving out the asking job. With a cutoff, jobs not
	// updated since then (no heartbeat) are left out.
	listIDs := func(updatedSince time.Time) []uuid.UUID {
		t.Helper()
		got, err := ds.ListActiveTurnJobsForChat(ctx, userID, chatA, self.ID, updatedSince)
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(got))
		for _, j := range got {
			ids = append(ids, j.ID)
			require.Equal(t, userID, j.UserID)
			require.False(t, j.CreatedAt.IsZero())
			require.False(t, j.UpdatedAt.IsZero())
		}
		return ids
	}
	require.Equal(t, []uuid.UUID{replied.ID, checkpointing.ID, webhook.ID, syncRun.ID}, listIDs(time.Now().Add(-2*time.Minute)),
		"unfinished, recently updated turns in the chat; a turn past its reply still blocks until it finishes")
	require.Equal(t, []uuid.UUID{replied.ID, checkpointing.ID, abandoned.ID, webhook.ID, syncRun.ID}, listIDs(time.Time{}),
		"a zero cutoff does not filter on updated_at")

	// Another user never sees them.
	stranger := createJobTestUser(t, ds)
	got, err := ds.ListActiveTurnJobsForChat(ctx, stranger, chatA, uuid.Nil, time.Time{})
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
	require.Equal(t, []uuid.UUID{self.ID, syncRun.ID, webhook.ID, abandoned.ID, checkpointing.ID, replied.ID}, stopIDs)
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
