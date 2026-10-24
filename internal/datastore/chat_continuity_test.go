package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A turn saves its continuity after the user changed the thread mid-reply (#347): only the
// response chain and last-message time move; the user's changes stand.
func TestUpdateChatContinuity_LeavesUserEditsAlone(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	before, after := uuid.New(), uuid.New()
	createTestPersonality(t, ds, before, userID)
	createTestPersonality(t, ds, after, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetPersonalityID(before).Exec(ctx))

	// The user switches personality and renames the thread while the turn runs.
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetPersonalityID(after).SetName("mine").SetDisabledTools([]string{"web_search"}).Exec(ctx))

	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, ds.UpdateChatContinuity(ctx, userID, chatID, "resp_123", &at))

	chat, err := ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, after, chat.PersonalityID)
	require.Equal(t, "mine", chat.Name)
	require.Equal(t, []string{"web_search"}, chat.DisabledTools)
	require.NotNil(t, chat.ResponseID)
	require.Equal(t, "resp_123", *chat.ResponseID)
	require.NotNil(t, chat.LastMessageTime)
	require.True(t, at.Equal(*chat.LastMessageTime))

	require.ErrorIs(t, ds.UpdateChatContinuity(ctx, uuid.New(), chatID, "resp_other", nil), ErrChatNotFound,
		"another user's chat is not written")
}

func TestRenameChatIfNamed_OnlyReplacesThePlaceholder(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetName("New thread").Exec(ctx))

	renamed, err := ds.RenameChatIfNamed(ctx, userID, chatID, "Generated title", "New thread", "New Chat")
	require.NoError(t, err)
	require.True(t, renamed)

	// The user typed a name while the first reply generated: the generated title must not win.
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetName("My name").Exec(ctx))
	renamed, err = ds.RenameChatIfNamed(ctx, userID, chatID, "Another title", "New thread", "New Chat")
	require.NoError(t, err)
	require.False(t, renamed)

	chat, err := ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, "My name", chat.Name)
}
