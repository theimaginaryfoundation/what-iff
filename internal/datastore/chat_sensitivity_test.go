package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entuser "github.com/theimaginaryfoundation/what-iff/ent/user"
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
	require.NoError(t, ds.dbClient.Personality.UpdateOneID(personalityID).SetScratchpad("the personality's private notes").Exec(ctx))
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetPersonalityID(personalityID).Exec(ctx))

	chat, err := ds.GetChat(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivitySensitive, chat.MemorySensitivityLimit, "the default is unrestricted")
	require.False(t, chat.MemoryRestricted())
	require.Equal(t, "the personality's private notes", chat.Scratchpad)

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

// A chat imported from an account export keeps today's default when the export has no limit, and
// fails closed (public) when it carries a value that is not a level.
func TestImportedChatMemoryLimit_InvalidFailsClosed(t *testing.T) {
	_, ok := importedChatMemoryLimit("")
	require.False(t, ok, "an old export without a limit keeps the chat default")

	for raw, want := range map[models.MemorySensitivity]string{
		"public":    "public",
		"personal":  "personal",
		"sensitive": "sensitive",
		"garbage":   "public",
		"Sensitive": "public",
		" personal": "public",
	} {
		limit, ok := importedChatMemoryLimit(raw)
		require.True(t, ok, raw)
		require.Equal(t, want, string(limit), "export limit %q", raw)
	}
}

// A thread's limit is also its own classification: a restricted chat lists only conversations
// whose own limit is at or below its own, and can look up another chat's limit (owner-scoped).
func TestChatLimitClassification_ListFilterAndLookup(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID, otherUser := uuid.New(), uuid.New()
	createTestUser(t, ds, userID)
	createTestUser(t, ds, otherUser)

	ids := map[models.MemorySensitivity]uuid.UUID{}
	for _, l := range models.AllMemorySensitivities {
		id := uuid.New()
		createTestChat(t, ds, id, userID)
		require.NoError(t, ds.dbClient.Chat.UpdateOneID(id).SetMemorySensitivityLimit(entchat.MemorySensitivityLimit(l)).Exec(ctx))
		ids[l] = id
	}

	// ListChats applies chatLimitAtMost for ChatFilters.MaxMemorySensitivityLimit; the hand-written
	// test schema has no chat_messages table for the rest of ListChats, so query the predicate.
	listed := func(limit *models.MemorySensitivity) []uuid.UUID {
		q := ds.dbClient.Chat.Query().Where(entchat.HasOwnerWith(entuser.ID(userID)))
		if limit != nil {
			if p := chatLimitAtMost(*limit); p != nil {
				q = q.Where(p)
			}
		}
		out, err := q.IDs(ctx)
		require.NoError(t, err)
		return out
	}
	pub, per, sens := models.MemorySensitivityPublic, models.MemorySensitivityPersonal, models.MemorySensitivitySensitive
	require.ElementsMatch(t, []uuid.UUID{ids[pub]}, listed(&pub))
	require.ElementsMatch(t, []uuid.UUID{ids[pub], ids[per]}, listed(&per))
	require.ElementsMatch(t, []uuid.UUID{ids[pub], ids[per], ids[sens]}, listed(&sens), "an unrestricted limit adds no filter")
	require.ElementsMatch(t, []uuid.UUID{ids[pub], ids[per], ids[sens]}, listed(nil))

	got, err := ds.GetChatMemorySensitivityLimit(ctx, userID, ids[per])
	require.NoError(t, err)
	require.Equal(t, per, got)
	_, err = ds.GetChatMemorySensitivityLimit(ctx, otherUser, ids[per])
	require.ErrorIs(t, err, ErrChatNotFound, "another user's chat is not found")
	_, err = ds.GetChatMemorySensitivityLimit(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrChatNotFound)
}

// A checkpoint summary carries its own level: the default capped by the chat's limit, never
// lowered on a later update (it is cumulative).
func TestUpsertChatSummaryMemory_SensitivityFollowsTheChatLimit(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()
	userID := uuid.New()
	createTestUser(t, ds, userID)

	level := func(chatID uuid.UUID) models.MemorySensitivity {
		sum, err := ds.GetChatSummaryMemory(ctx, userID, chatID)
		require.NoError(t, err)
		require.NotNil(t, sum)
		return sum.Sensitivity
	}
	setLimit := func(chatID uuid.UUID, l models.MemorySensitivity) {
		require.NoError(t, ds.dbClient.Chat.UpdateOneID(chatID).SetMemorySensitivityLimit(entchat.MemorySensitivityLimit(l)).Exec(ctx))
	}

	ordinary := uuid.New()
	createTestChat(t, ds, ordinary, userID)
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, ordinary, "summary", []float32{0.1}))
	require.Equal(t, models.MemorySensitivityPersonal, level(ordinary), "an ordinary thread's summary is personal")

	// Lowering the limit later does not lower the summary: it still holds what was said before.
	setLimit(ordinary, models.MemorySensitivityPublic)
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, ordinary, "summary 2", []float32{0.2}))
	require.Equal(t, models.MemorySensitivityPersonal, level(ordinary))

	public := uuid.New()
	createTestChat(t, ds, public, userID)
	setLimit(public, models.MemorySensitivityPublic)
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, public, "public summary", []float32{0.1}))
	require.Equal(t, models.MemorySensitivityPublic, level(public), "a public thread's summary is public")

	// Raising the limit raises the summary on the next update.
	setLimit(public, models.MemorySensitivitySensitive)
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, public, "public summary 2", []float32{0.2}))
	require.Equal(t, models.MemorySensitivityPersonal, level(public))
}
