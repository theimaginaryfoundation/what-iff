package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Datastore halves of the issue #254 concurrency fixes: the per-chat turn-job listing the turn gate
// polls, and the scratchpad revision that makes checkpoint writes conditional.

func TestListActiveTurnJobsForChat(t *testing.T) {
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
	first := createJob("chat_message", turnA1.String(), models.JobStatusInferenceComplete)
	webhook := createJob("agent_job_run", chatA.String(), models.JobStatusProcessing)
	syncRun := createJob("agent_job_run", turnA2.String(), models.JobStatusPending)
	last := createJob("chat_message", turnA2.String(), models.JobStatusPending)
	// Not turns in chatA: another thread's turn, a non-turn job keyed on the chat id, and a job
	// whose reference is neither a chat nor a message.
	createJob("chat_message", turnB.String(), models.JobStatusProcessing)
	createJob("agent_job_run", chatB.String(), models.JobStatusProcessing)
	createJob("thread_rehydration", chatA.String(), models.JobStatusProcessing)
	createJob("chat_message", "not-a-uuid", models.JobStatusProcessing)

	got, err := ds.ListActiveTurnJobsForChat(ctx, userID, chatA)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(got))
	for _, j := range got {
		ids = append(ids, j.ID)
		require.False(t, j.CreatedAt.IsZero())
		require.False(t, j.UpdatedAt.IsZero())
	}
	require.Equal(t, []uuid.UUID{first.ID, webhook.ID, syncRun.ID, last.ID}, ids,
		"live turn jobs in the chat, oldest first, whether keyed by chat or by message")

	// Another user never sees them.
	stranger := createJobTestUser(t, ds)
	got, err = ds.ListActiveTurnJobsForChat(ctx, stranger, chatA)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestScratchpadRevision_ConditionalWrite(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)
	revision := func() (string, int, []string) {
		t.Helper()
		p, err := ds.dbClient.Personality.Get(ctx, personalityID)
		require.NoError(t, err)
		return p.Scratchpad, p.ScratchpadRevision, p.ScratchpadHistory
	}
	_, rev, _ := revision()
	require.Equal(t, 0, rev)

	// A write at the current revision lands and bumps it, keeping history.
	updated, err := ds.UpdatePersonalityScratchpadIfRevision(ctx, userID, personalityID, "notes v1", 0)
	require.NoError(t, err)
	require.Equal(t, "notes v1", updated.Scratchpad)
	require.Equal(t, 1, updated.ScratchpadRevision)

	// An unconditional write (user edit, update_scratchpad tool) also bumps it...
	updated, err = ds.UpdatePersonalityScratchpad(ctx, userID, models.Personality{ID: personalityID, Scratchpad: "notes v2"})
	require.NoError(t, err)
	require.Equal(t, 2, updated.ScratchpadRevision)

	// ...so a checkpoint still holding revision 1 conflicts and changes nothing.
	_, err = ds.UpdatePersonalityScratchpadIfRevision(ctx, userID, personalityID, "stale rewrite", 1)
	require.ErrorIs(t, err, ErrScratchpadConflict)
	content, rev, history := revision()
	require.Equal(t, "notes v2", content)
	require.Equal(t, 2, rev)
	require.Equal(t, "notes v1", history[0])

	// Ownership is still enforced; the owner's retry at the latest revision lands.
	stranger := createJobTestUser(t, ds)
	_, err = ds.UpdatePersonalityScratchpadIfRevision(ctx, stranger, personalityID, "x", 2)
	require.ErrorIs(t, err, ErrPersonalityNotFound)
	_, err = ds.UpdatePersonalityScratchpadIfRevision(ctx, userID, personalityID, "notes v3", 2)
	require.NoError(t, err)
	content, rev, _ = revision()
	require.Equal(t, "notes v3", content)
	require.Equal(t, 3, rev)
}
