package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// newSupersedeTestDatastore has what listing messages needs (newChatMessageTestDatastore) plus jobs.
func newSupersedeTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createFileAttachmentTestSchema,
		alterChatsTableForAgentJobTests, createChatMessageRitualsTestSchema, createJobTestSchema)
}

// supersedeFixture is one user's chat with messages at known, increasing times.
type supersedeFixture struct {
	ds     *Datastore
	userID uuid.UUID
	chatID uuid.UUID
	base   time.Time
	n      int
}

func newSupersedeFixture(t *testing.T, ds *Datastore) *supersedeFixture {
	t.Helper()
	userID := createCMTestUser(t, ds)
	chatID := createCMTestChat(t, ds, userID, createCMTestModel(t, ds))
	return &supersedeFixture{ds: ds, userID: userID, chatID: chatID, base: time.Now().UTC().Add(-time.Hour)}
}

// add saves a message one second after the previous one.
func (f *supersedeFixture) add(t *testing.T, origin models.MessageOrigin, text string) uuid.UUID {
	t.Helper()
	f.n++
	m, err := f.ds.dbClient.ChatMessage.Create().
		SetChatID(f.chatID).
		SetMessage(text).
		SetOrigin(chatmessage.Origin(origin)).
		SetSentAt(f.base.Add(time.Duration(f.n) * time.Second)).
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

// listed returns the texts ListChatMessages returns, oldest first.
func (f *supersedeFixture) listed(t *testing.T, filters models.ChatMessageFilters) []string {
	t.Helper()
	page, err := f.ds.ListChatMessages(context.Background(), f.userID, f.chatID, 1, 50, filters)
	require.NoError(t, err)
	out := make([]string, 0, len(page.Results))
	for i := len(page.Results) - 1; i >= 0; i-- {
		out = append(out, page.Results[i].(*models.ChatMessage).Message)
	}
	return out
}

func (f *supersedeFixture) get(t *testing.T, id uuid.UUID) *models.ChatMessage {
	t.Helper()
	m, err := f.ds.GetChatMessage(context.Background(), f.userID, id)
	require.NoError(t, err)
	return m
}

// takeBack takes back the turn userMessageID starts, as userID (SupersedeTurn).
func (f *supersedeFixture) takeBack(userID, userMessageID uuid.UUID) error {
	_, err := f.ds.SupersedeTurn(context.Background(), userID, userMessageID, time.Now())
	return err
}

// regenerateErr regenerates the turn userMessageID starts, as userID (RegenerateTurn).
func (f *supersedeFixture) regenerateErr(userID, userMessageID uuid.UUID) error {
	_, err := f.ds.RegenerateTurn(context.Background(), userID, userMessageID, false, time.Now())
	return err
}

// regenerate regenerates the turn userMessageID starts and returns the new run's job.
func (f *supersedeFixture) regenerate(t *testing.T, userMessageID uuid.UUID) *models.Job {
	t.Helper()
	j, err := f.ds.RegenerateTurn(context.Background(), f.userID, userMessageID, false, time.Now())
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond) // runs created after it are ordered after it by created_at
	return j
}

// finish marks a run complete, as its worker does once the turn is done.
func (f *supersedeFixture) finish(t *testing.T, run *models.Job) {
	t.Helper()
	_, err := f.ds.UpdateJobStatus(context.Background(), f.userID, run.ID, models.JobStatusComplete, "")
	require.NoError(t, err)
}

func TestSupersedeTurn_HidesTheTurnEverywhereMessagesAreRead(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	f.add(t, models.MessageOriginUser, "u1")
	f.add(t, models.MessageOriginAssistant, "a1")
	u2 := f.add(t, models.MessageOriginUser, "u2")
	a2 := f.add(t, models.MessageOriginAssistant, "a2")
	require.NoError(t, ds.UpdateChatContinuity(ctx, f.userID, f.chatID, "resp_a2", nil))

	require.NoError(t, f.takeBack(f.userID, u2))

	require.Equal(t, []string{"u1", "a1"}, f.listed(t, models.ChatMessageFilters{}), "the model's history and the message routes list through here")
	require.Equal(t, []string{"u1", "a1", "u2", "a2"}, f.listed(t, models.ChatMessageFilters{IncludeSuperseded: true}), "past versions are kept")

	n, err := ds.GetChatMessageCount(ctx, f.userID, f.chatID, models.MessageOriginFilterAll)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	since, err := ds.FirstChatMessageSentAtSince(ctx, f.userID, f.chatID, f.base.Add(3*time.Second))
	require.NoError(t, err)
	require.Nil(t, since, "the checkpoint window start does not see a superseded message")

	latest, err := ds.GetLatestMessagesForChats(ctx, f.userID, []uuid.UUID{f.chatID})
	require.NoError(t, err)
	require.Equal(t, "a1", latest[f.chatID])

	transcript, err := ds.GetChatMessagesForSummary(ctx, f.userID, f.chatID)
	require.NoError(t, err)
	require.Len(t, transcript, 2, "thread rehydration's transcript")

	// Still readable by id, marked, with nothing that replaced it.
	for _, id := range []uuid.UUID{u2, a2} {
		m := f.get(t, id)
		require.NotNil(t, m.SupersededAt)
		require.Nil(t, m.SupersededBy)
	}

	chat, err := ds.dbClient.Chat.Get(ctx, f.chatID)
	require.NoError(t, err)
	require.Empty(t, chat.ResponseID, "no response chain continues from the superseded reply")
}

func TestSupersedeTurn_OnlyTheLatestTurn(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	a1 := f.add(t, models.MessageOriginAssistant, "a1")
	u2 := f.add(t, models.MessageOriginUser, "u2")
	f.add(t, models.MessageOriginAssistant, "a2")

	require.ErrorIs(t, f.takeBack(f.userID, u1), ErrTurnNotLatest)
	require.ErrorIs(t, f.regenerateErr(f.userID, u1), ErrTurnNotLatest)
	_, err := ds.CheckTurnRevisable(ctx, f.userID, u1)
	require.ErrorIs(t, err, ErrTurnNotLatest)
	require.ErrorIs(t, f.takeBack(f.userID, a1), ErrTurnNotUserMessage)
	require.ErrorIs(t, f.takeBack(createCMTestUser(t, ds), u2), ErrChatMessageNotFound)
	require.ErrorIs(t, f.takeBack(f.userID, uuid.New()), ErrChatMessageNotFound)
	require.Equal(t, []string{"u1", "a1", "u2", "a2"}, f.listed(t, models.ChatMessageFilters{}), "a refused supersede changes nothing")

	require.NoError(t, f.takeBack(f.userID, u2))
	require.NoError(t, f.takeBack(f.userID, u2), "superseding a superseded turn is a no-op")
	require.ErrorIs(t, f.regenerateErr(f.userID, u2), ErrTurnSuperseded, "a turn taken back cannot be regenerated")
	turn, err := ds.CheckTurnRevisable(ctx, f.userID, u2)
	require.NoError(t, err)
	require.True(t, turn.Superseded)

	// With u2 taken back, u1 is the latest turn again.
	turn, err = ds.CheckTurnRevisable(ctx, f.userID, u1)
	require.NoError(t, err)
	require.False(t, turn.Superseded)
	require.Equal(t, f.chatID, turn.ChatID)
	require.NoError(t, f.takeBack(f.userID, u1))
	require.Empty(t, f.listed(t, models.ChatMessageFilters{}))

	has, err := ds.ChatHasMessages(ctx, f.userID, f.chatID, models.MessageOriginFilterUser)
	require.NoError(t, err)
	require.False(t, has, "with every turn taken back, the next one is the chat's first again (it gets the user-name line)")
}

func TestSupersedeTurn_RegenerateLinksTheNewReply(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	a1 := f.add(t, models.MessageOriginAssistant, "a1")
	first := createActiveChatMessageJob(t, ds, f.userID, u1.String(), models.JobStatusComplete)
	_, err := ds.SetJobResult(ctx, f.userID, first.ID, a1)
	require.NoError(t, err)

	// A later turn taken back outright: its reply must not be linked to u1's new reply.
	u2 := f.add(t, models.MessageOriginUser, "u2")
	a2 := f.add(t, models.MessageOriginAssistant, "a2")
	require.NoError(t, f.takeBack(f.userID, u2))

	second := f.regenerate(t, u1)
	require.Equal(t, "chat_message", second.JobType)
	require.Equal(t, u1.String(), second.Reference)
	require.Equal(t, models.JobStatusPending, second.Status)
	require.Equal(t, []string{"u1"}, f.listed(t, models.ChatMessageFilters{}), "the user message stays, to be answered again")
	require.NotNil(t, f.get(t, a1).SupersededAt)
	require.Nil(t, f.get(t, a1).SupersededBy, "linked only once the new reply is saved")

	// The new run's reply (sent after the superseded u2) is saved and settled; it is recorded on
	// its run.
	a3 := f.add(t, models.MessageOriginAssistant, "a3")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, second.ID, a3))
	run, err := ds.GetJob(ctx, f.userID, second.ID)
	require.NoError(t, err)
	require.Equal(t, a3, *run.ResultID)

	require.Equal(t, []string{"u1", "a3"}, f.listed(t, models.ChatMessageFilters{}))
	require.Equal(t, a3, *f.get(t, a1).SupersededBy)
	require.Nil(t, f.get(t, a2).SupersededBy, "a reply taken back with its own turn was replaced by nothing")
	require.Nil(t, f.get(t, a3).SupersededAt)

	// Regenerating again supersedes a3 (positioned after u2, still u1's reply).
	f.finish(t, second)
	third := f.regenerate(t, u1)
	require.NotNil(t, f.get(t, a3).SupersededAt)
	a4 := f.add(t, models.MessageOriginAssistant, "a4")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, third.ID, a4))
	require.Equal(t, a3, *f.get(t, a1).SupersededBy, "an earlier link is kept")
	require.Equal(t, a4, *f.get(t, a3).SupersededBy)
	require.Equal(t, []string{"u1", "a4"}, f.listed(t, models.ChatMessageFilters{}))
}

func TestSettleTurnReply_ALateReplyOfATurnTakenBackIsSuperseded(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	f.add(t, models.MessageOriginUser, "u1")
	f.add(t, models.MessageOriginAssistant, "a1")
	u2 := f.add(t, models.MessageOriginUser, "u2")
	require.NoError(t, f.takeBack(f.userID, u2))

	// The stopped run's reply lands after the supersede.
	late := f.add(t, models.MessageOriginAssistant, "late")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u2, uuid.Nil, late))
	require.NotNil(t, f.get(t, late).SupersededAt)
	require.Equal(t, []string{"u1", "a1"}, f.listed(t, models.ChatMessageFilters{}))
}

func TestSettleTurnReply_OrdinaryTurnIsLeftAlone(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	a1 := f.add(t, models.MessageOriginAssistant, "a1")
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, u1, uuid.Nil, a1))
	// A reference that is not a user message (a scheduled run's is its chat) is a no-op.
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, f.chatID, uuid.Nil, a1))
	require.NoError(t, ds.SettleTurnReply(ctx, f.userID, f.chatID, a1, uuid.Nil, a1))

	m := f.get(t, a1)
	require.Nil(t, m.SupersededAt)
	require.Nil(t, m.SupersededBy)
	require.Equal(t, []string{"u1", "a1"}, f.listed(t, models.ChatMessageFilters{}))
}

func TestFinalizeCancelledChatJobWithPartial_TurnTakenBackSupersedesThePartial(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	f.add(t, models.MessageOriginUser, "u1")
	f.add(t, models.MessageOriginAssistant, "a1")
	u2 := f.add(t, models.MessageOriginUser, "u2")
	jobModel := baseJobModel()
	jobModel.Reference = u2.String()
	jobModel.Status = models.JobStatusProcessing
	jobModel.DraftDeltas = []string{"half a ", "reply"}
	running, err := ds.CreateJob(ctx, f.userID, jobModel)
	require.NoError(t, err)

	require.NoError(t, f.takeBack(f.userID, u2))
	_, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, f.userID, running.ID, f.chatID, "m", "p", nil)
	require.NoError(t, err)
	require.NotNil(t, resultID)
	require.NotNil(t, f.get(t, *resultID).SupersededAt, "the stopped run's partial reply is not live")
	require.Equal(t, []string{"u1", "a1"}, f.listed(t, models.ChatMessageFilters{}))
}

func TestSupersedeTurn_ScheduledRunRepliesStayLive(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	f.add(t, models.MessageOriginAssistant, "a1")
	f.n++
	_, err := ds.CreateChatMessage(ctx, f.userID, models.ChatMessage{
		ChatID: f.chatID, Message: "scheduled", Origin: models.MessageOriginAssistant,
		SentAt: f.base.Add(time.Duration(f.n) * time.Second),
		JobRun: &models.JobRun{AgentJobID: uuid.New(), Prompt: "check in"},
	})
	require.NoError(t, err)

	require.NoError(t, f.takeBack(f.userID, u1))
	require.Equal(t, []string{"scheduled"}, f.listed(t, models.ChatMessageFilters{}), "a scheduled run answers its own prompt, not the turn")
}

func TestLatestChatMessageJobIDAndExcludeFromMemory(t *testing.T) {
	ds, cleanup := newSupersedeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	f := newSupersedeFixture(t, ds)

	u1 := f.add(t, models.MessageOriginUser, "u1")
	id, err := ds.LatestChatMessageJobID(ctx, f.userID, u1)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, id)

	createActiveChatMessageJob(t, ds, f.userID, u1.String(), models.JobStatusFailed)
	newest := createActiveChatMessageJob(t, ds, f.userID, u1.String(), models.JobStatusPending)
	id, err = ds.LatestChatMessageJobID(ctx, f.userID, u1)
	require.NoError(t, err)
	require.Equal(t, newest.ID, id)

	require.NoError(t, ds.MarkChatMessageExcludedFromMemory(ctx, f.userID, u1))
	require.True(t, f.get(t, u1).ExcludeFromMemory)
	turn, err := ds.CheckTurnRevisable(ctx, f.userID, u1)
	require.NoError(t, err)
	require.True(t, turn.ExcludedFromMemory)
	require.ErrorIs(t, ds.MarkChatMessageExcludedFromMemory(ctx, createCMTestUser(t, ds), u1), ErrChatMessageNotFound)
}
