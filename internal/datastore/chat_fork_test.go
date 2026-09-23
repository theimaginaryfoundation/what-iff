package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func newChatForkTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createMCPServerTestSchema, alterChatsTableForAgentJobTests)
}

type forkFixture struct {
	userID, modelID, personalityID, chatID uuid.UUID
	// messageIDs are in conversation order: u0, a0, u1, a1, ...
	messageIDs []uuid.UUID
	base       time.Time
}

// seedForkThread creates a chat with `turns` user/assistant pairs one minute apart.
func seedForkThread(t *testing.T, ds *Datastore, turns int) forkFixture {
	t.Helper()
	ctx := context.Background()
	f := forkFixture{base: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	f.userID = createAgentJobTestUser(t, ds)
	f.modelID = createAgentJobTestModel(t, ds)
	f.personalityID = createAgentJobTestPersonality(t, ds, f.userID)

	chat, err := ds.dbClient.Chat.Create().
		SetName("Trip plans").
		SetOwnerID(f.userID).
		SetModelID(f.modelID).
		SetPersonalityID(f.personalityID).
		SetTags([]string{"travel"}).
		SetIsAutoMood(false).
		Save(ctx)
	require.NoError(t, err)
	f.chatID = chat.ID

	for i := 0; i < turns; i++ {
		for j, origin := range []chatmessage.Origin{chatmessage.OriginUser, chatmessage.OriginAssistant} {
			m, err := ds.dbClient.ChatMessage.Create().
				SetChatID(chat.ID).
				SetMessage(string(origin) + " " + string(rune('A'+i))).
				SetOrigin(origin).
				SetSentAt(f.base.Add(time.Duration(i*2+j) * time.Minute)).
				SetGenerationModel("gpt-test").
				SetResponseID("resp-" + uuid.NewString()).
				SetBookmarked(true).
				Save(ctx)
			require.NoError(t, err)
			f.messageIDs = append(f.messageIDs, m.ID)
		}
	}
	return f
}

func branchMessages(t *testing.T, ds *Datastore, chatID uuid.UUID) []chatmessageRow {
	t.Helper()
	rows, err := ds.dbClient.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID))).
		Order(chatmessage.BySentAt()).
		All(context.Background())
	require.NoError(t, err)
	out := make([]chatmessageRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, chatmessageRow{Message: r.Message, Origin: string(r.Origin), SentAt: r.SentAt, ResponseID: r.ResponseID, Bookmarked: r.Bookmarked})
	}
	return out
}

type chatmessageRow struct {
	Message    string
	Origin     string
	SentAt     time.Time
	ResponseID string
	Bookmarked bool
}

func TestForkChat_CopiesPrefixThroughAssistantMessage(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 3)

	// Branch after the second assistant reply (index 3 = a1).
	res, err := ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{
		ParentChatID:   f.chatID,
		MessageID:      f.messageIDs[3],
		IncludeMessage: true,
	})
	require.NoError(t, err)
	require.Equal(t, 4, res.CopiedMessages)
	require.False(t, res.NeedsRehydration)

	branch := res.Chat
	require.NotEqual(t, f.chatID, branch.ID)
	require.Equal(t, "What if: Trip plans", branch.Name)
	require.Equal(t, f.modelID, branch.ModelID)
	require.Equal(t, f.personalityID, branch.PersonalityID)
	require.Equal(t, []string{"travel"}, branch.Tags)
	require.False(t, branch.IsAutoMood)
	require.NotNil(t, branch.ForkedFromChatID)
	require.Equal(t, f.chatID, *branch.ForkedFromChatID)
	require.Equal(t, f.messageIDs[3], *branch.ForkedFromMessageID)
	require.NotNil(t, branch.LastMessageTime)
	require.True(t, branch.LastMessageTime.Equal(f.base.Add(3*time.Minute)))

	msgs := branchMessages(t, ds, branch.ID)
	require.Len(t, msgs, 4)
	for _, m := range msgs {
		require.Empty(t, m.ResponseID, "provider response ids are not copied")
		require.False(t, m.Bookmarked, "bookmarks are not copied")
		require.True(t, m.SentAt.Before(f.base.Add(4*time.Minute)))
	}

	// The parent is untouched.
	require.Len(t, branchMessages(t, ds, f.chatID), 6)
}

func TestForkChat_ExcludeMessageEndsBeforeUserTurn(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 3)

	// "What if I'd said…" on u1 (index 2): branch holds u0, a0 only.
	res, err := ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{
		ParentChatID: f.chatID,
		MessageID:    f.messageIDs[2],
		Name:         "Plan B",
	})
	require.NoError(t, err)
	require.Equal(t, 2, res.CopiedMessages)
	require.Equal(t, "Plan B", res.Chat.Name)
	require.Len(t, branchMessages(t, ds, res.Chat.ID), 2)
}

func TestForkChat_ExcludingFirstMessageCreatesEmptyBranch(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 1)

	res, err := ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{
		ParentChatID: f.chatID,
		MessageID:    f.messageIDs[0],
	})
	require.NoError(t, err)
	require.Zero(t, res.CopiedMessages)
	require.False(t, res.NeedsRehydration)
	require.Empty(t, branchMessages(t, ds, res.Chat.ID))
}

func TestForkChat_ReusesCheckpointWhenBranchIsAfterIt(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 4)
	checkpointAt := f.base.Add(2 * time.Minute) // window starts at u1
	_, err := ds.dbClient.Chat.UpdateOneID(f.chatID).
		SetCheckpointSummary("They chose Lisbon.").
		SetCheckpointUserMessageCount(1).
		SetLastCheckpointAt(checkpointAt).
		Save(context.Background())
	require.NoError(t, err)

	res, err := ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{
		ParentChatID:   f.chatID,
		MessageID:      f.messageIDs[5], // a2, after the checkpoint
		IncludeMessage: true,
	})
	require.NoError(t, err)
	require.False(t, res.NeedsRehydration)

	row, err := ds.dbClient.Chat.Get(context.Background(), res.Chat.ID)
	require.NoError(t, err)
	require.Equal(t, "They chose Lisbon.", row.CheckpointSummary)
	require.Equal(t, 1, row.CheckpointUserMessageCount)
	require.NotNil(t, row.LastCheckpointAt)
	require.True(t, row.LastCheckpointAt.Equal(checkpointAt))
	require.Empty(t, row.RehydrationState)
}

func TestForkChat_RehydratesWhenCheckpointIsAfterBranchPoint(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 4)
	_, err := ds.dbClient.Chat.UpdateOneID(f.chatID).
		SetCheckpointSummary("Summary that mentions the future.").
		SetCheckpointUserMessageCount(3).
		SetLastCheckpointAt(f.base.Add(6 * time.Minute)).
		Save(context.Background())
	require.NoError(t, err)

	res, err := ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{
		ParentChatID:   f.chatID,
		MessageID:      f.messageIDs[1], // a0, before the checkpoint
		IncludeMessage: true,
	})
	require.NoError(t, err)
	require.True(t, res.NeedsRehydration)

	row, err := ds.dbClient.Chat.Get(context.Background(), res.Chat.ID)
	require.NoError(t, err)
	require.Empty(t, row.CheckpointSummary, "the parent's summary covers turns the branch never had")
	require.Nil(t, row.LastCheckpointAt)
	require.Equal(t, models.RehydrationStatePending, row.RehydrationState)
}

func TestForkChat_NotFoundCases(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 1)
	other := seedForkThread(t, ds, 1)

	_, err := ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{ParentChatID: uuid.New(), MessageID: f.messageIDs[0]})
	require.ErrorIs(t, err, ErrChatNotFound)

	// Another user's chat is invisible.
	_, err = ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{ParentChatID: other.chatID, MessageID: other.messageIDs[0]})
	require.ErrorIs(t, err, ErrChatNotFound)

	// A message from a different thread can't be the branch point.
	_, err = ds.ForkChat(context.Background(), f.userID, models.ForkChatParams{ParentChatID: f.chatID, MessageID: other.messageIDs[0]})
	require.ErrorIs(t, err, ErrForkMessageNotFound)
}

func TestListChatBranchesAndGetChatName(t *testing.T) {
	t.Parallel()
	ds, cleanup := newChatForkTestDatastore(t)
	defer cleanup()
	f := seedForkThread(t, ds, 2)
	ctx := context.Background()

	first, err := ds.ForkChat(ctx, f.userID, models.ForkChatParams{ParentChatID: f.chatID, MessageID: f.messageIDs[1], IncludeMessage: true, Name: "one"})
	require.NoError(t, err)
	second, err := ds.ForkChat(ctx, f.userID, models.ForkChatParams{ParentChatID: f.chatID, MessageID: f.messageIDs[2], Name: "two"})
	require.NoError(t, err)
	// A branch of a branch is listed under its own parent, not the root.
	_, err = ds.ForkChat(ctx, f.userID, models.ForkChatParams{ParentChatID: first.Chat.ID, MessageID: firstMessageID(t, ds, first.Chat.ID), IncludeMessage: true})
	require.NoError(t, err)

	branches, err := ds.ListChatBranches(ctx, f.userID, f.chatID)
	require.NoError(t, err)
	require.Len(t, branches, 2)
	ids := map[uuid.UUID]string{}
	for _, b := range branches {
		ids[b.ID] = b.Name
		require.NotNil(t, b.ForkedFromMessageID)
	}
	require.Equal(t, "one", ids[first.Chat.ID])
	require.Equal(t, "two", ids[second.Chat.ID])

	// Another user sees none of them.
	other := seedForkThread(t, ds, 1)
	none, err := ds.ListChatBranches(ctx, other.userID, f.chatID)
	require.NoError(t, err)
	require.Empty(t, none)

	name, err := ds.GetChatName(ctx, f.userID, f.chatID)
	require.NoError(t, err)
	require.Equal(t, "Trip plans", name)
	_, err = ds.GetChatName(ctx, other.userID, f.chatID)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func firstMessageID(t *testing.T, ds *Datastore, chatID uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := ds.dbClient.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID))).
		Order(chatmessage.BySentAt()).
		FirstID(context.Background())
	require.NoError(t, err)
	return id
}

func TestForkNeedsRehydration(t *testing.T) {
	t.Parallel()
	branchPoint := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	before, after := branchPoint.Add(-time.Hour), branchPoint.Add(time.Hour)
	imported := "openai"

	cases := []struct {
		name   string
		parent models.Chat
		copied int
		want   bool
	}{
		{"empty branch never rehydrates", models.Chat{LastCheckpointAt: &after}, 0, false},
		{"native thread without checkpoint", models.Chat{}, 10, false},
		{"unsummarized import", models.Chat{Source: &imported}, 10, true},
		{"checkpoint before branch point is reusable", models.Chat{LastCheckpointAt: &before}, 10, false},
		{"checkpoint at branch point is reusable", models.Chat{LastCheckpointAt: &branchPoint}, 10, false},
		{"checkpoint after branch point leaks the future", models.Chat{LastCheckpointAt: &after}, 10, true},
		{"parent still rehydrating", models.Chat{LastCheckpointAt: &before, RehydrationState: models.RehydrationStateProcessing}, 10, true},
		{"parent rehydration failed", models.Chat{Source: &imported, RehydrationState: models.RehydrationStateFailed}, 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := tc.parent
			require.Equal(t, tc.want, forkNeedsRehydration(&parent, branchPoint, tc.copied))
		})
	}
}

func TestForkChatName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "What if: Trip plans", forkChatName("", "Trip plans"))
	require.Equal(t, "What if: Untitled thread", forkChatName("  ", " "))
	require.Equal(t, "Custom", forkChatName("  Custom ", "Trip plans"))
	long := forkChatName("", string(make([]rune, 300)))
	require.LessOrEqual(t, len([]rune(long)), maxForkNameLength)
}
