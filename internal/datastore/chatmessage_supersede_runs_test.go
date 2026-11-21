package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Taking a turn back, settled by run (chatmessage_supersede.go): late replies of a stopped run,
// one run per regenerate, only the turn's own runs and replies.

// A Stop handled on another instance marks the run cancelled at once, but its worker can still
// save a reply a moment later. A regenerate in that window starts a newer run; the stopped run's
// late reply is superseded on arrival, so the turn never has two live replies.
func TestSettleTurnReply_ALateReplyOfARunRegeneratedSinceIsSuperseded(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	stopped := createActiveChatMessageJob(t, ds, f.userID, u1.String(), models.JobStatusProcessing)
	changed, err := ds.MarkChatJobCancelled(ctx, f.userID, stopped.ID)
	require.NoError(t, err)
	require.True(t, changed)
	active, err := ds.FindActiveTurnJobForChat(ctx, f.userID, f.chatID)
	require.NoError(t, err)
	require.Nil(t, active, "the stopped run no longer counts as running")

	regen := f.regenerate(t, u1)

	// The stopped run's worker saves its reply after the regenerate.
	late := f.add(t, models.MessageOriginAssistant, "late")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, stopped.ID, late))
	require.NotNil(t, f.get(t, late).SupersededAt, "a run that is no longer the turn's newest saves no live reply")
	require.Equal(t, []string{"u1"}, f.listed(t, models.ChatMessageFilters{}))

	// The regenerate's reply is live, and recorded as what replaced the late one.
	fresh := f.add(t, models.MessageOriginAssistant, "fresh")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, regen.ID, fresh))
	require.Equal(t, []string{"u1", "fresh"}, f.listed(t, models.ChatMessageFilters{}))
	require.Equal(t, fresh, *f.get(t, late).SupersededBy)

	// A late reply arriving after the new one is linked to it at once.
	later := f.add(t, models.MessageOriginAssistant, "later")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, stopped.ID, later))
	require.Equal(t, fresh, *f.get(t, later).SupersededBy)
	require.Equal(t, []string{"u1", "fresh"}, f.listed(t, models.ChatMessageFilters{}))
}

// The partial path: a run whose worker was taken for dead (no heartbeat for TurnJobStaleAfter) does
// not block a regenerate; should it wake and save a partial reply after all, the partial is
// superseded in its own transaction.
func TestFinalizeChatJobWithPartial_ARunRegeneratedSinceSavesNoLivePartial(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	jobModel := baseJobModel()
	jobModel.Reference = u1.String()
	jobModel.Status = models.JobStatusProcessing
	jobModel.DraftDeltas = []string{"half a ", "reply"}
	stale, err := ds.CreateJob(ctx, f.userID, jobModel)
	require.NoError(t, err)
	require.NoError(t, ds.dbClient.Job.UpdateOneID(stale.ID).SetUpdatedAt(time.Now().Add(-2*TurnJobStaleAfter)).Exec(ctx))
	time.Sleep(5 * time.Millisecond)

	regen := f.regenerate(t, u1)
	_, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, f.userID, stale.ID, f.chatID, "m", "p", nil)
	require.NoError(t, err)
	require.NotNil(t, resultID)
	require.NotNil(t, f.get(t, *resultID).SupersededAt, "the woken run's partial is not live")
	require.Equal(t, []string{"u1"}, f.listed(t, models.ChatMessageFilters{}))

	fresh := f.add(t, models.MessageOriginAssistant, "fresh")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, regen.ID, fresh))
	require.Equal(t, []string{"u1", "fresh"}, f.listed(t, models.ChatMessageFilters{}))
}

// The check that no run of the turn is unfinished and the new run's job are one transaction under
// the user message's lock, so a second regenerate (a double click, a retried request) sees the
// first's run and starts none.
func TestRegenerateTurn_StartsOneRunAtATime(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	f.add(t, models.MessageOriginAssistant, "a1")
	first := f.regenerate(t, u1)
	require.ErrorIs(t, f.regenerateErr(f.userID, u1), ErrTurnRunning)

	runs, err := ds.dbClient.Job.Query().Where(turnRuns(f.userID, u1)...).All(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1, "one run for two regenerates")
	require.Equal(t, first.ID, runs[0].ID)

	// Every unfinished status holds it, a post-inference phase too; a finished run does not.
	for _, st := range []models.JobStatus{models.JobStatusProcessing, models.JobStatusInferenceComplete, models.JobStatusCompactionComplete} {
		_, err := ds.UpdateJobStatus(ctx, f.userID, first.ID, st, "")
		require.NoError(t, err)
		require.ErrorIs(t, f.regenerateErr(f.userID, u1), ErrTurnRunning, string(st))
	}
	f.finish(t, first)
	second := f.regenerate(t, u1)
	require.NotEqual(t, first.ID, second.ID)

	// The synchronous path's run (agent_job_run keyed by the user message) is one of the turn's.
	f.finish(t, second)
	createActiveTurnJob(t, ds, f.userID, "agent_job_run", u1.String(), models.JobStatusProcessing)
	require.ErrorIs(t, f.regenerateErr(f.userID, u1), ErrTurnRunning)
}

// RegenerateTurn flags the user message out of memory when asked, and clears its last error.
func TestRegenerateTurn_FlagsAndClearsTheUserMessage(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	boom := "boom"
	require.NoError(t, ds.SetChatMessageLastError(ctx, f.userID, u1, &boom, "code"))
	_, err := ds.RegenerateTurn(ctx, f.userID, u1, true, time.Now())
	require.NoError(t, err)
	m := f.get(t, u1)
	require.True(t, m.ExcludeFromMemory)
	require.Nil(t, m.SupersededAt)
	require.Nil(t, m.LastErrorMessage)
}

// SupersedeTurn returns only the turn's own unfinished runs to stop, not a scheduled run in the
// chat or an earlier turn still answering.
func TestSupersedeTurn_ReturnsOnlyTheTurnsUnfinishedRuns(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	earlier := createActiveChatMessageJob(t, ds, f.userID, u1.String(), models.JobStatusProcessing)
	scheduled := createActiveTurnJob(t, ds, f.userID, "agent_job_run", f.chatID.String(), models.JobStatusProcessing)
	u2 := f.add(t, models.MessageOriginUser, "u2")
	queued := createActiveChatMessageJob(t, ds, f.userID, u2.String(), models.JobStatusPending)
	createActiveChatMessageJob(t, ds, f.userID, u2.String(), models.JobStatusFailed)

	_, err := ds.SupersedeTurn(context.Background(), f.userID, u1, time.Now())
	require.ErrorIs(t, err, ErrTurnNotLatest)
	running, err := ds.SupersedeTurn(context.Background(), f.userID, u2, time.Now())
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{queued.ID}, running)
	require.NotContains(t, running, earlier.ID)
	require.NotContains(t, running, scheduled.ID)

	// Taken back already: still reported, so a stop that failed can be tried again.
	running, err = ds.SupersedeTurn(context.Background(), f.userID, u2, time.Now())
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{queued.ID}, running)
}

// A turn's replies are what its runs saved. Assistant messages after the user message that its
// runs did not save (a webhook's post, say) stay live. A turn with no run (imported history) falls
// back to position (TestSupersedeTurn_HidesTheTurnEverywhereMessagesAreRead).
func TestSupersedeTurn_OnlyTheTurnsRunsReplies(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	run := createActiveChatMessageJob(t, ds, f.userID, u1.String(), models.JobStatusProcessing)
	a1 := f.add(t, models.MessageOriginAssistant, "a1")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, run.ID, a1))
	f.add(t, models.MessageOriginAssistant, "webhook post")
	f.finish(t, run)

	require.NoError(t, f.takeBack(f.userID, u1))
	require.Equal(t, []string{"webhook post"}, f.listed(t, models.ChatMessageFilters{}))
	require.NotNil(t, f.get(t, a1).SupersededAt)
}

// The synchronous path runs a user message as an agent_job_run keyed by the message. Its reply and
// its partial are the turn's, as a chat_message run's are.
func TestSupersedeTurn_SyncPathRunIsOneOfTheTurns(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	f.add(t, models.MessageOriginUser, "u1")
	f.add(t, models.MessageOriginAssistant, "a1")
	u2 := f.add(t, models.MessageOriginUser, "u2")
	sync := createActiveTurnJob(t, ds, f.userID, "agent_job_run", u2.String(), models.JobStatusComplete)
	a2 := f.add(t, models.MessageOriginAssistant, "a2")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u2, sync.ID, a2))
	run, err := ds.GetJob(ctx, f.userID, sync.ID)
	require.NoError(t, err)
	require.Equal(t, a2, *run.ResultID, "recorded on its run")
	f.add(t, models.MessageOriginAssistant, "webhook post")

	require.NoError(t, f.takeBack(f.userID, u2))
	require.Equal(t, []string{"u1", "a1", "webhook post"}, f.listed(t, models.ChatMessageFilters{}))

	// A partial a sync run saves after its turn was taken back is superseded too.
	jobModel := baseJobModel()
	jobModel.JobType = "agent_job_run"
	jobModel.Reference = u2.String()
	jobModel.Status = models.JobStatusProcessing
	jobModel.DraftDeltas = []string{"partial"}
	late, err := ds.CreateJob(ctx, f.userID, jobModel)
	require.NoError(t, err)
	_, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, f.userID, late.ID, f.chatID, "m", "p", nil)
	require.NoError(t, err)
	require.NotNil(t, resultID)
	require.NotNil(t, f.get(t, *resultID).SupersededAt)
}

// A bookmark on a superseded message is not listed (the UI navigator, and the recall tool's
// bookmarks mode).
func TestChatMessageBookmarks_LeaveOutSupersededMessages(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	a1 := f.add(t, models.MessageOriginAssistant, "a1")
	u2 := f.add(t, models.MessageOriginUser, "u2")
	a2 := f.add(t, models.MessageOriginAssistant, "a2")
	for _, id := range []uuid.UUID{u1, a1, u2, a2} {
		_, err := ds.SetChatMessageBookmarked(ctx, f.userID, id, true)
		require.NoError(t, err)
	}
	require.NoError(t, f.takeBack(f.userID, u2))

	all, err := ds.ListChatMessageBookmarks(ctx, f.userID, f.chatID)
	require.NoError(t, err)
	got := make([]string, 0, len(all))
	for _, m := range all {
		got = append(got, m.Message)
	}
	require.Equal(t, []string{"u1", "a1"}, got)

	page, err := ds.ListChatMessageBookmarksPage(ctx, f.userID, f.chatID, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Results, 2)
}
