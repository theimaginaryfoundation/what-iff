package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Taking a turn back (turn_supersede.go): what the next turn sees, and the latest-only and
// running rules.

// supersedeTurns saves messages in a chat one second apart.
type supersedeTurns struct {
	t      *testing.T
	ds     *datastore.Datastore
	userID uuid.UUID
	chatID uuid.UUID
	base   time.Time
	n      int
}

func (s *supersedeTurns) add(origin models.MessageOrigin, text string, edit ...func(*models.ChatMessage)) *models.ChatMessage {
	s.t.Helper()
	s.n++
	msg := models.ChatMessage{ChatID: s.chatID, Message: text, Origin: origin, SentAt: s.base.Add(time.Duration(s.n) * time.Second)}
	for _, e := range edit {
		e(&msg)
	}
	saved, err := s.ds.CreateChatMessage(context.Background(), s.userID, msg)
	require.NoError(s.t, err)
	return saved
}

func newSupersedeTurns(t *testing.T) (*Agent, *supersedeTurns, *models.Chat) {
	t.Helper()
	a, ds, _, userID, chat := newSkillsAgentFixture(t)
	return a, &supersedeTurns{t: t, ds: ds, userID: userID, chatID: chat.ID, base: time.Now().UTC().Add(-time.Hour)}, chat
}

func buildNextTurn(t *testing.T, a *Agent, userID uuid.UUID, chat *models.Chat, current *models.ChatMessage) (*provider.ModelContext, builtTurn) {
	t.Helper()
	b, err := newMessageContextBuilder(a.ds, &telemetry.Telemetry{Logger: zap.NewNop()}, nil, nil, nil)
	require.NoError(t, err)
	mc, built, err := b.buildTurn(context.Background(), messageContextBuildRequest{UserID: userID, Chat: chat, UserPrompt: current.Message, CurrentMessage: current})
	require.NoError(t, err)
	return mc, built
}

func historyText(t *testing.T, a *Agent, userID uuid.UUID, chat *models.Chat, current *models.ChatMessage) (string, builtTurn) {
	t.Helper()
	mc, built := buildNextTurn(t, a, userID, chat, current)
	return segmentText(mc, provider.SegmentKindHistoryTurn), built
}

// The next turn's context has neither message of a superseded turn, from the history or from the
// checkpoint carry-over, and a superseded save-off turn no longer keeps memories from being
// extracted from the rest (it is not in the conversation the checkpoint replays).
func TestSupersededTurn_IsLeftOutOfTheNextTurnsContext(t *testing.T) {
	a, s, chat := newSupersedeTurns(t)
	ctx := context.Background()
	s.add(models.MessageOriginUser, "KEEP-U1")
	s.add(models.MessageOriginAssistant, "KEEP-A1")
	u2 := s.add(models.MessageOriginUser, "GONE-U2", func(m *models.ChatMessage) { m.ExcludeFromMemory = true })
	s.add(models.MessageOriginAssistant, "GONE-A2", func(m *models.ChatMessage) {
		m.ExcludeFromMemory = true
		m.AgentRun = &models.AgentRun{EndReason: models.AgentRunEndStepLimit, Plan: []models.PlanItem{{Content: "GONE-PLAN", Status: models.PlanItemPending}}}
	})

	plan, _ := a.previousRunOpenPlan(ctx, s.userID, chat.ID)
	require.Len(t, plan, 1, "before the supersede, the stopped run's plan carries over")
	require.NoError(t, s.ds.UpdateChatContinuity(ctx, s.userID, chat.ID, "resp_gone_a2", nil))

	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u2.ID))
	u3 := s.add(models.MessageOriginUser, "NOW-U3")

	history, built := historyText(t, a, s.userID, chat, u3)
	assert.Contains(t, history, "KEEP-U1")
	assert.Contains(t, history, "KEEP-A1")
	assert.NotContains(t, history, "GONE")
	assert.False(t, built.replaysExcludedTurn, "a superseded save-off turn is not in the replayed conversation")

	// The same through the checkpoint carry-over (everything before the checkpoint).
	checkpointed := *chat
	cp := s.base.Add(time.Duration(s.n)*time.Second - time.Second/2)
	checkpointed.LastCheckpointAt = &cp
	history, built = historyText(t, a, s.userID, &checkpointed, u3)
	assert.Contains(t, history, "KEEP-U1")
	assert.NotContains(t, history, "GONE")
	assert.False(t, built.replaysExcludedTurn)

	// OpenAI: the next turn's request carries its whole context and continues no response chain;
	// the chat's stored response id no longer names the superseded reply's response.
	mc, _ := buildNextTurn(t, a, s.userID, chat, u3)
	params := mc.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{Model: "gpt-test"})
	assert.False(t, params.PreviousResponseID.Valid(), "no previous_response_id")
	raw, err := params.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(raw), "KEEP-A1")
	assert.NotContains(t, string(raw), "GONE")
	saved, err := s.ds.GetChat(ctx, s.userID, chat.ID)
	require.NoError(t, err)
	assert.True(t, saved.ResponseID == nil || *saved.ResponseID == "")

	plan, _ = a.previousRunOpenPlan(ctx, s.userID, chat.ID)
	assert.Empty(t, plan, "an Advanced run's open plan does not carry over from a superseded reply")
	assert.False(t, a.isFirstUserMessageInChat(ctx, s.userID, chat.ID, u3.ID))
}

// When the only turn was taken back, the next one is the chat's first again and gets the
// user-name line: the superseded message that carried it is no longer in the context.
func TestSupersededOnlyTurn_NextTurnGetsTheUserNameLine(t *testing.T) {
	a, s, chat := newSupersedeTurns(t)
	ctx := context.Background()
	u1 := s.add(models.MessageOriginUser, "u1")
	s.add(models.MessageOriginAssistant, "a1")

	lines, err := a.turnUserNameLines(ctx, s.userID, chat.ID, false)
	require.NoError(t, err)
	assert.Empty(t, lines)

	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u1.ID))
	lines, err = a.turnUserNameLines(ctx, s.userID, chat.ID, false)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.True(t, strings.HasPrefix(lines[0], userNameContextPrefix))
}

func TestSupersedeTurn_OnlyTheLatestTurn(t *testing.T) {
	a, s, _ := newSupersedeTurns(t)
	ctx := context.Background()
	u1 := s.add(models.MessageOriginUser, "u1")
	a1 := s.add(models.MessageOriginAssistant, "a1")
	u2 := s.add(models.MessageOriginUser, "u2")

	require.ErrorIs(t, a.SupersedeTurn(ctx, s.userID, u1.ID), ErrTurnNotLatest)
	require.ErrorIs(t, a.SupersedeTurn(ctx, s.userID, a1.ID), ErrTurnNotUserMessage)
	require.ErrorIs(t, a.SupersedeTurn(ctx, uuid.New(), u2.ID), datastore.ErrChatMessageNotFound)

	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u2.ID))
	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u2.ID), "idempotent")
	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u1.ID), "u1 is the latest turn again")
}

// The turn's runs are stopped as it is taken back: in this process through their cancel func, and
// a run with no local worker (another instance, or orphaned) by marking it cancelled. Only the
// turn's own: a scheduled run in the chat and an earlier turn still answering keep running, and a
// take-back that is refused stops nothing.
func TestSupersedeTurn_StopsOnlyTheTurnsRuns(t *testing.T) {
	a, s, chat := newSupersedeTurns(t)
	ctx := context.Background()
	newJob := func(jobType, reference string, status models.JobStatus) *models.Job {
		t.Helper()
		j, err := s.ds.CreateJob(ctx, s.userID, models.Job{JobType: jobType, Reference: reference, Status: status, DraftDeltas: []string{}})
		require.NoError(t, err)
		return j
	}
	statusOf := func(j *models.Job) models.JobStatus {
		t.Helper()
		st, err := s.ds.JobStatus(ctx, s.userID, j.ID)
		require.NoError(t, err)
		return st
	}

	u1 := s.add(models.MessageOriginUser, "u1")
	earlier := newJob(JobTypeChatMessage, u1.ID.String(), models.JobStatusProcessing)
	scheduled := newJob(JobTypeAgentJobRun, chat.ID.String(), models.JobStatusProcessing)
	u2 := s.add(models.MessageOriginUser, "u2")
	elsewhere := newJob(JobTypeChatMessage, u2.ID.String(), models.JobStatusProcessing)
	local := newJob(JobTypeChatMessage, u2.ID.String(), models.JobStatusPending)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.registerRunningJobCancel(local.ID, s.userID, cancel)

	require.ErrorIs(t, a.SupersedeTurn(ctx, s.userID, u1.ID), ErrTurnNotLatest)
	assert.Equal(t, models.JobStatusProcessing, statusOf(earlier), "a refused take-back stops nothing")

	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u2.ID))
	assert.ErrorIs(t, runCtx.Err(), context.Canceled, "the run in this process is cancelled")
	assert.Equal(t, models.JobStatusCancelled, statusOf(elsewhere), "a run elsewhere is cancelled through the database")
	assert.Equal(t, models.JobStatusProcessing, statusOf(earlier), "an earlier turn still answering keeps running")
	assert.Equal(t, models.JobStatusProcessing, statusOf(scheduled), "a scheduled run in the chat keeps running")
	msg, err := s.ds.GetChatMessage(ctx, s.userID, u2.ID)
	require.NoError(t, err)
	assert.NotNil(t, msg.SupersededAt)
}

func TestRegenerateTurn_Rules(t *testing.T) {
	a, s, _ := newSupersedeTurns(t)
	ctx := middleware.ContextWithUser(context.Background(), s.userID, "")
	u1 := s.add(models.MessageOriginUser, "u1")
	a1 := s.add(models.MessageOriginAssistant, "a1")
	u2 := s.add(models.MessageOriginUser, "u2")

	_, err := a.RegenerateTurn(context.Background(), u2.ID, nil)
	require.Error(t, err, "the user comes from the context")
	_, err = a.RegenerateTurn(ctx, u1.ID, nil)
	require.ErrorIs(t, err, ErrTurnNotLatest)
	_, err = a.RegenerateTurn(ctx, a1.ID, nil)
	require.ErrorIs(t, err, ErrTurnNotUserMessage)

	running, err := s.ds.CreateJob(ctx, s.userID, models.Job{JobType: JobTypeChatMessage, Reference: u2.ID.String(), Status: models.JobStatusProcessing, DraftDeltas: []string{}})
	require.NoError(t, err)
	_, err = a.RegenerateTurn(ctx, u2.ID, nil)
	require.ErrorIs(t, err, ErrTurnRunning, "a running turn is not regenerated (supersede stops it instead)")
	_, err = s.ds.UpdateJobStatus(ctx, s.userID, running.ID, models.JobStatusFailed, "boom")
	require.NoError(t, err)

	require.NoError(t, a.SupersedeTurn(ctx, s.userID, u2.ID))
	_, err = a.RegenerateTurn(ctx, u2.ID, nil)
	require.ErrorIs(t, err, ErrTurnSuperseded)
	_, err = a.RetryUserChatMessage(ctx, u2.ChatID, u2.ID)
	require.ErrorIs(t, err, datastore.ErrChatMessageNotFound, "the app's retry does not rerun a turn taken back")
}

// Regenerate supersedes the reply, keeps the user message, and starts a new chat_message job for
// it with the new options. This agent has no model, so the run itself fails; what is checked is
// what regenerate did before it.
func TestRegenerateTurn_StartsANewRunForTheSameMessage(t *testing.T) {
	a, s, chat := newSupersedeTurns(t)
	ctx := middleware.ContextWithUser(context.Background(), s.userID, "")
	u1 := s.add(models.MessageOriginUser, "u1")
	a1 := s.add(models.MessageOriginAssistant, "a1")
	first, err := s.ds.CreateJob(ctx, s.userID, models.Job{JobType: JobTypeChatMessage, Reference: u1.ID.String(), Status: models.JobStatusComplete, DraftDeltas: []string{}, ResultID: &a1.ID})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond) // the new run is ordered after it by created_at

	resp, err := a.RegenerateTurn(ctx, u1.ID, &models.TurnOptions{MemorySave: ptrTo(false)})
	require.NoError(t, err)
	assert.Equal(t, u1.ID, resp.ID)
	assert.Equal(t, JobTypeChatMessage, resp.Type)
	jobID := uuid.MustParse(resp.JobID)
	assert.NotEqual(t, first.ID, jobID)
	latest, err := s.ds.LatestChatMessageJobID(ctx, s.userID, u1.ID)
	require.NoError(t, err)
	assert.Equal(t, jobID, latest)

	// Wait for the run itself to finish (its in-process registration) before reading its job.
	require.Eventually(t, func() bool { return !a.hasRunningJob(jobID) }, 30*time.Second, 20*time.Millisecond,
		"the run finishes (it fails: no model)")
	job, err := s.ds.GetJob(context.Background(), s.userID, jobID)
	require.NoError(t, err)
	assert.Contains(t, []models.JobStatus{models.JobStatusFailed, models.JobStatusComplete, models.JobStatusCancelled}, job.Status)

	reply, err := s.ds.GetChatMessage(ctx, s.userID, a1.ID)
	require.NoError(t, err)
	assert.NotNil(t, reply.SupersededAt)
	user, err := s.ds.GetChatMessage(ctx, s.userID, u1.ID)
	require.NoError(t, err)
	assert.Nil(t, user.SupersededAt, "the user message stays, answered again")
	assert.True(t, user.ExcludeFromMemory, "memory saving off for the new run flags the message")

	page, err := s.ds.ListChatMessages(ctx, s.userID, chat.ID, 1, 10, models.ChatMessageFilters{})
	require.NoError(t, err)
	for _, r := range page.Results {
		assert.NotEqual(t, a1.ID, r.(*models.ChatMessage).ID)
	}
}

func TestSettleTurnReply_NilSafe(t *testing.T) {
	(&Agent{logger: zap.NewNop()}).settleTurnReply(context.Background(), uuid.New(), nil, &models.ChatMessage{}, uuid.New())
	a, s, _ := newSupersedeTurns(t)
	a.settleTurnReply(context.Background(), s.userID, nil, nil, uuid.New())
	a.settleTurnReply(context.Background(), s.userID, &chatContext{turnID: "not-a-uuid"}, &models.ChatMessage{ID: uuid.New(), ChatID: s.chatID}, uuid.Nil)
	assert.Equal(t, uuid.Nil, (*chatContext)(nil).turnJobID())
	id := uuid.New()
	assert.Equal(t, id, (&chatContext{turnID: id.String()}).turnJobID())
}

// The agent settles a reply by the run that saved it (the turn's job on chatCtx): a reply of a run
// a regenerate replaced is superseded as it is saved.
func TestSettleTurnReply_ByTheRunThatSavedIt(t *testing.T) {
	a, s, _ := newSupersedeTurns(t)
	ctx := context.Background()
	u1 := s.add(models.MessageOriginUser, "u1")
	stopped, err := s.ds.CreateJob(ctx, s.userID, models.Job{JobType: JobTypeChatMessage, Reference: u1.ID.String(), Status: models.JobStatusCancelled, DraftDeltas: []string{}})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	_, err = s.ds.RegenerateTurn(ctx, s.userID, u1.ID, false, time.Now())
	require.NoError(t, err)

	late := s.add(models.MessageOriginAssistant, "late")
	a.settleTurnReply(ctx, s.userID, &chatContext{turnID: stopped.ID.String()}, u1, late.ID)
	got, err := s.ds.GetChatMessage(ctx, s.userID, late.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.SupersededAt)
}

// A regenerate whose options name a model the sender cannot use is refused before anything is
// taken back or started, as a new send with one is refused before anything is stored.
func TestRegenerateTurn_RefusesAnUnavailableModelFirst(t *testing.T) {
	a, s, _ := newSupersedeTurns(t)
	ctx := middleware.ContextWithUser(context.Background(), s.userID, "")
	u1 := s.add(models.MessageOriginUser, "u1")
	a1 := s.add(models.MessageOriginAssistant, "a1")

	missing := uuid.New()
	_, err := a.RegenerateTurn(ctx, u1.ID, &models.TurnOptions{ModelID: &missing})
	require.ErrorIs(t, err, ErrTurnModelUnavailable)
	got, err := s.ds.GetChatMessage(context.Background(), s.userID, a1.ID)
	require.NoError(t, err)
	assert.Nil(t, got.SupersededAt, "the reply stays live")
	jobID, err := s.ds.LatestChatMessageJobID(context.Background(), s.userID, u1.ID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, jobID, "no run was started")
}
