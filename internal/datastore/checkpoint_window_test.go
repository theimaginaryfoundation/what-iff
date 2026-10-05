package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
)

// Issue #268: the checkpoint's history window (last_checkpoint_at) starts after the last message the
// summary covers, not when the checkpoint finished.

func createCheckpointWindowTestMessage(t *testing.T, ds *Datastore, chatID uuid.UUID, origin string, sentAt time.Time) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.ChatMessage.Create().
		SetChatID(chatID).
		SetMessage(origin + " message").
		SetOrigin(chatmessage.Origin(origin)).
		SetSentAt(sentAt).
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func TestFirstChatMessageSentAtSince(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	chatID, otherChat := uuid.New(), uuid.New()
	createTestChat(t, ds, chatID, userID)
	createTestChat(t, ds, otherChat, userID)
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	createCheckpointWindowTestMessage(t, ds, chatID, "User", t0.Add(-time.Minute)) // before since
	user := createCheckpointWindowTestMessage(t, ds, chatID, "User", t0)
	reply := createCheckpointWindowTestMessage(t, ds, chatID, "Assistant", t0.Add(10*time.Second))
	createCheckpointWindowTestMessage(t, ds, otherChat, "User", t0.Add(time.Second)) // another chat

	got, err := ds.FirstChatMessageSentAtSince(ctx, userID, chatID, t0, user, reply)
	require.NoError(t, err)
	require.Nil(t, got, "the turn's own messages are left out, and other chats never count")

	next := t0.Add(30 * time.Second)
	createCheckpointWindowTestMessage(t, ds, chatID, "User", next)
	got, err = ds.FirstChatMessageSentAtSince(ctx, userID, chatID, t0, user, reply)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.Equal(next), "got %s", got)

	got, err = ds.FirstChatMessageSentAtSince(ctx, userID, chatID, t0)
	require.NoError(t, err)
	require.True(t, got.Equal(t0), "with nothing excluded the user message itself is first")

	stranger := createJobTestUser(t, ds)
	got, err = ds.FirstChatMessageSentAtSince(ctx, stranger, chatID, t0)
	require.NoError(t, err)
	require.Nil(t, got, "another user's chat")
}

// The #268 scenario end to end: turn N is checkpointed while the user's next message arrives. With
// the window starting just after N's reply (agent.checkpointWindowStart), the next message stays in
// the history the following turns load; the old cursor, the time the checkpoint finished, hid it.
func TestCheckpointWindow_MessageSentDuringTheCheckpointStaysInHistory(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	t0 := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	createCheckpointWindowTestMessage(t, ds, chatID, "User", t0)
	reply := t0.Add(10 * time.Second)
	createCheckpointWindowTestMessage(t, ds, chatID, "Assistant", reply)
	next := createCheckpointWindowTestMessage(t, ds, chatID, "User", t0.Add(20*time.Second)) // sent mid-checkpoint

	require.NoError(t, ds.UpdateChatCheckpointStateAndClearResponseID(ctx, userID, chatID, "summary of turn N", 1, reply.Add(time.Microsecond)))

	row, err := ds.dbClient.Chat.Get(ctx, chatID)
	require.NoError(t, err)
	require.NotNil(t, row.LastCheckpointAt)
	require.True(t, row.LastCheckpointAt.Equal(reply.Add(time.Microsecond)), "stored the given window start, got %s", row.LastCheckpointAt)
	require.Equal(t, "summary of turn N", row.CheckpointSummary)

	// The history query the context builder runs (sent_at >= last_checkpoint_at).
	ids, err := ds.dbClient.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID)), chatmessage.SentAtGTE(*row.LastCheckpointAt)).
		IDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{next}, ids, "only the unsummarized next message is live; turn N is in the summary")

	// A zero window start keeps the old behaviour (now).
	before := time.Now()
	require.NoError(t, ds.UpdateChatCheckpointStateAndClearResponseID(ctx, userID, chatID, "summary", 2, time.Time{}))
	row, err = ds.dbClient.Chat.Get(ctx, chatID)
	require.NoError(t, err)
	require.False(t, row.LastCheckpointAt.Before(before.Truncate(time.Microsecond)))
}
