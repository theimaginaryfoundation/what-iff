package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestChat_ContextScope_DefaultUpdateAndScratchpadGate(t *testing.T) {
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
	require.Equal(t, models.ContextScopeAccount, chat.ContextScope, "a chat reads the account by default")
	require.False(t, chat.IsSandboxed())
	require.Equal(t, "the personality's private notes", chat.Scratchpad)

	// PATCH-style update: omitting the scope keeps it.
	updated, err := ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "renamed", PersonalityID: personalityID})
	require.NoError(t, err)
	require.False(t, updated.IsSandboxed())

	// The datastore itself writes any explicit scope (the HTTP handlers are what refuse to sandbox
	// an existing chat); from here on the chat is a sandbox, and the loader stops handing out the
	// scratchpad.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "renamed", PersonalityID: personalityID, ContextScope: models.ContextScopeSandbox, SetContextScope: true})
	require.NoError(t, err)
	require.True(t, updated.IsSandboxed())
	require.Empty(t, updated.Scratchpad, "a sandboxed chat is never given the personality scratchpad")

	chat, err = ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.True(t, chat.IsSandboxed())
	require.Empty(t, chat.Scratchpad)
	require.Equal(t, personalityID, chat.PersonalityID, "the rest of the chat is unchanged")

	// The context view of a sandboxed chat has no scratchpad either (and the handler refuses a
	// save), so nothing in the sandbox reads or writes the personality's notes.
	chatContext, err := ds.GetChatContext(ctx, userID, chatID)
	require.NoError(t, err)
	require.Empty(t, chatContext.ActiveScratchpad)
	require.NotEmpty(t, chatContext.ChatID)

	// Later updates that do not mention the scope keep it.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID})
	require.NoError(t, err)
	require.True(t, updated.IsSandboxed())

	// A turn that loaded the chat BEFORE the scope changed saves its copy afterwards (turn
	// bookkeeping, naming): the stale, unflagged value must not un-sandbox it.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "stale turn copy", PersonalityID: personalityID, ContextScope: models.ContextScopeAccount})
	require.NoError(t, err)
	require.True(t, updated.IsSandboxed(), "only an explicit change writes the scope")

	// An unknown scope is refused.
	_, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID, ContextScope: "project", SetContextScope: true})
	require.ErrorIs(t, err, ErrInvalidRequestBody)

	// Leaving the sandbox restores the scratchpad.
	_, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID, SetContextScope: true})
	require.NoError(t, err)
	chat, err = ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.False(t, chat.IsSandboxed())
	require.Equal(t, "the personality's private notes", chat.Scratchpad)
}

// A chat created sandboxed starts with the registered default-off tools in its disabled_tools,
// so every creation path (the API, a plugin, an import) starts a sandbox the same way; a caller
// that passes its own list keeps it, and an ordinary chat gets none.
func TestCreateChat_SandboxStartsWithTheDefaultOffTools(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	_, err := ds.dbClient.UserPreference.Create().SetUserID(userID).SetDefaultModel(modelID).Save(ctx)
	require.NoError(t, err)
	models.RegisterSandboxDefaultDisabledTool("sandbox_test_tool")

	sandboxed, err := ds.CreateChat(ctx, userID, models.Chat{Name: "relay", ContextScope: models.ContextScopeSandbox})
	require.NoError(t, err)
	require.True(t, sandboxed.IsSandboxed())
	require.Contains(t, sandboxed.DisabledTools, "sandbox_test_tool")

	own, err := ds.CreateChat(ctx, userID, models.Chat{Name: "relay", ContextScope: models.ContextScopeSandbox, DisabledTools: []string{"web_search"}})
	require.NoError(t, err)
	require.Equal(t, []string{"web_search"}, own.DisabledTools)

	plain, err := ds.CreateChat(ctx, userID, models.Chat{Name: "plain"})
	require.NoError(t, err)
	require.False(t, plain.IsSandboxed())
	require.Empty(t, plain.DisabledTools)

	_, err = ds.CreateChat(ctx, userID, models.Chat{Name: "odd", ContextScope: "project"})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
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
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(sandboxedID).SetContextScope(entchat.ContextScopeSandbox).Exec(ctx))

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
