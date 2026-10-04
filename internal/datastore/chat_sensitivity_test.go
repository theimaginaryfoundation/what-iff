package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestChat_MemorySensitivityLimit_DefaultUpdateAndScratchpadGate(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)
	require.NoError(t, ds.dbClient.Personality.UpdateOneID(personalityID).SetScratchpad("the personality's private notes").SetScratchpadRevision(4).Exec(ctx))
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetPersonalityID(personalityID).Exec(ctx))

	chat, err := ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivitySensitive, chat.MemorySensitivityLimit, "the default is unrestricted")
	require.False(t, chat.MemoryRestricted())
	require.Equal(t, "the personality's private notes", chat.Scratchpad)
	require.Equal(t, 4, chat.ScratchpadRevision)

	// PATCH-style update: omitting the limit keeps it.
	updated, err := ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "renamed", PersonalityID: personalityID})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivitySensitive, updated.MemorySensitivityLimit)

	// Lowering the limit takes effect immediately, and the loader stops handing out the scratchpad.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "renamed", PersonalityID: personalityID, MemorySensitivityLimit: models.MemorySensitivityPublic, SetMemorySensitivityLimit: true})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivityPublic, updated.MemorySensitivityLimit)
	require.Empty(t, updated.Scratchpad, "a restricted chat is never given the personality scratchpad")

	chat, err = ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.True(t, chat.MemoryRestricted())
	require.Empty(t, chat.Scratchpad)
	require.Zero(t, chat.ScratchpadRevision)
	require.Equal(t, personalityID, chat.PersonalityID, "the rest of the chat is unchanged")

	// Later updates that do not mention the limit keep it restricted.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivityPublic, updated.MemorySensitivityLimit)

	// A turn that loaded the chat BEFORE the limit was lowered saves its copy afterwards (turn
	// bookkeeping, naming): the stale, unflagged limit must not raise it again.
	updated, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "stale turn copy", PersonalityID: personalityID, MemorySensitivityLimit: models.MemorySensitivitySensitive})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivityPublic, updated.MemorySensitivityLimit, "only an explicit change writes the limit")

	// Raising it back restores the scratchpad.
	_, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", PersonalityID: personalityID, MemorySensitivityLimit: models.MemorySensitivitySensitive, SetMemorySensitivityLimit: true})
	require.NoError(t, err)
	chat, err = ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, "the personality's private notes", chat.Scratchpad)

	_, err = ds.UpdateChat(ctx, userID, models.Chat{ID: chatID, Name: "again", MemorySensitivityLimit: "bogus", SetMemorySensitivityLimit: true})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}
