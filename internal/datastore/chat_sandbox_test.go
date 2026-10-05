package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestChat_Sandboxed_DefaultUpdateAndScratchpadGate(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)
	require.NoError(t, ds.dbClient.Personality.UpdateOneID(personalityID).SetScratchpad("the personality's private notes").Exec(ctx))
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetPersonalityID(personalityID).Exec(ctx))

	chat, err := ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.False(t, chat.Sandboxed, "a chat is not sandboxed by default")
	require.Equal(t, "the personality's private notes", chat.Scratchpad)

	// PATCH-style update: omitting the flag keeps it.
	updated, err := ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "renamed", PersonalityID: personalityID})
	require.NoError(t, err)
	require.False(t, updated.Sandboxed)

	// Sandboxing takes effect immediately, and the loader stops handing out the scratchpad.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "renamed", PersonalityID: personalityID, Sandboxed: true, SetSandboxed: true})
	require.NoError(t, err)
	require.True(t, updated.Sandboxed)
	require.Empty(t, updated.Scratchpad, "a sandboxed chat is never given the personality scratchpad")

	chat, err = ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.True(t, chat.IsSandboxed())
	require.Empty(t, chat.Scratchpad)
	require.Equal(t, personalityID, chat.PersonalityID, "the rest of the chat is unchanged")

	// The owner's own context view still shows the scratchpad, so saving it cannot wipe it.
	chatContext, err := ds.GetChatContext(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, "the personality's private notes", chatContext.ActiveScratchpad)

	// Later updates that do not mention the flag keep it.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID})
	require.NoError(t, err)
	require.True(t, updated.Sandboxed)

	// A turn that loaded the chat BEFORE it was sandboxed saves its copy afterwards (turn
	// bookkeeping, naming): the stale, unflagged value must not un-sandbox it.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "stale turn copy", PersonalityID: personalityID, Sandboxed: false})
	require.NoError(t, err)
	require.True(t, updated.Sandboxed, "only an explicit change writes the flag")

	// Turning it off restores the scratchpad.
	_, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID, SetSandboxed: true})
	require.NoError(t, err)
	chat, err = ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.False(t, chat.Sandboxed)
	require.Equal(t, "the personality's private notes", chat.Scratchpad)
}

// Whatever scope a writer asks for, a memory created from a sandboxed chat is Chat-scoped, so no
// write path can put sandbox-learned content into the owner's account-wide memories.
func TestCreateMemory_FromASandboxedChatIsAlwaysChatScoped(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	sandboxedID, ordinaryID := uuid.New(), uuid.New()
	createTestChat(t, ds, sandboxedID, userID)
	createTestChat(t, ds, ordinaryID, userID)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(sandboxedID).SetSandboxed(true).Exec(ctx))

	create := func(chatID uuid.UUID, scope string) *ent.Memory {
		t.Helper()
		mem, err := ds.CreateMemory(ctx, userID, models.Memory{ChatID: chatID, Content: "prefers metric units", Scope: scope}, nil, uuid.Nil)
		require.NoError(t, err)
		return mem
	}

	require.Equal(t, memory.ScopeChat, create(sandboxedID, "User").Scope, "a User-scoped write from a sandbox is forced to Chat")
	require.Equal(t, memory.ScopeChat, create(sandboxedID, "Chat").Scope)
	require.Equal(t, memory.ScopeUser, create(ordinaryID, "User").Scope, "control: an ordinary chat keeps the scope it asked for")
}
