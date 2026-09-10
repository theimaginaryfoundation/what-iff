package datastore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// createChatMessageRitualsTestSchema adds the chat_message_rituals m2m join table. CreateChatMessage
// (and its eager-loaded WithRituals()) touches this table whenever rituals are attached, but none of
// the other shared schema fragments define it.
func createChatMessageRitualsTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE chat_message_rituals (
		chat_message_id uuid NOT NULL,
		ritual_id uuid NOT NULL,
		PRIMARY KEY (chat_message_id, ritual_id)
	)`)
	require.NoError(t, err)
}

// newChatMessageTestDatastore composes the schema fragments chatmessage.go's queries need:
// createMemoryImportTestSchema (users/chats/personalities), createAccountBackupTestSchema
// (chat_messages, chat_message_context_items, tool_calls, moods), createFileAttachmentTestSchema
// (personality_expressions, for the generation-expression edge), alterChatsTableForAgentJobTests
// (GetChatMessage/CreateChatMessage/etc. all eager-load WithChat(), which selects every real chats
// column), and createChatMessageRitualsTestSchema (the rituals m2m join table).
func newChatMessageTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createFileAttachmentTestSchema, alterChatsTableForAgentJobTests, createChatMessageRitualsTestSchema)
}

func createCMTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("cm-" + id.String()[:8]).
		SetEmail("cm-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func createCMTestModel(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.Model.Create().
		SetName("model-" + uuid.NewString()[:8]).
		SetDisplayName("Test Model").
		SetDescription("test model").
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func createCMTestChat(t *testing.T, ds *Datastore, userID, modelID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	c, err := ds.dbClient.Chat.Create().
		SetName("Chat").
		SetOwnerID(userID).
		SetModelID(modelID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return c.ID
}

// TestToChatMessageModel_Nil documents the nil-safe conversion.
func TestToChatMessageModel_Nil(t *testing.T) {
	require.Nil(t, toChatMessageModel(nil))
}

func TestTrimGenerationExpressionReasoning(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty string", in: "", want: ""},
		{name: "whitespace only", in: "   \t\n  ", want: ""},
		{name: "trims surrounding whitespace", in: "  hello  ", want: "hello"},
		{name: "short string unchanged", in: "short reasoning", want: "short reasoning"},
		{name: "exactly at max runes", in: strings.Repeat("a", 512), want: strings.Repeat("a", 512)},
		{name: "over max runes is truncated", in: strings.Repeat("a", 600), want: strings.Repeat("a", 512)},
		{name: "truncation trims trailing whitespace", in: strings.Repeat("a", 511) + "  " + strings.Repeat("b", 100), want: strings.Repeat("a", 511)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimGenerationExpressionReasoning(tt.in)
			require.Equal(t, tt.want, got)
			require.LessOrEqual(t, len([]rune(got)), 512)
		})
	}
}

func TestCreateChatMessage_HappyPathWithContextItems(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	memoryID := uuid.New()
	msg := models.ChatMessage{
		ChatID:  chatID,
		Message: "hello there",
		Origin:  models.MessageOriginUser,
		Tokens:  12,
		AdditionalContext: []models.AdditionalContextItem{
			{Type: models.AdditionalContextTypeMemory, Content: "remembered fact", MemoryID: &memoryID, Scope: "personal"},
		},
	}

	created, err := ds.CreateChatMessage(ctx, userID, msg)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, "hello there", created.Message)
	require.Equal(t, models.MessageOriginUser, created.Origin)
	// User-origin messages are created already read.
	require.Equal(t, models.MessageReadStatusRead, created.ReadStatus)
	require.Len(t, created.AdditionalContext, 1)
	require.Equal(t, "remembered fact", created.AdditionalContext[0].Content)
	require.Equal(t, &memoryID, created.AdditionalContext[0].MemoryID)

	// Chat's last_message_time should have been bumped to the message's sent_at.
	chat, err := ds.dbClient.Chat.Get(ctx, chatID)
	require.NoError(t, err)
	require.NotNil(t, chat.LastMessageTime)
}

func TestCreateChatMessage_HappyPathNoContextItems(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	msg := models.ChatMessage{
		ChatID:  chatID,
		Message: "assistant reply",
		Origin:  models.MessageOriginAssistant,
	}

	created, err := ds.CreateChatMessage(ctx, userID, msg)
	require.NoError(t, err)
	require.NotNil(t, created)
	// Assistant-origin messages start unread.
	require.Equal(t, models.MessageReadStatusUnread, created.ReadStatus)
	require.Empty(t, created.AdditionalContext)
}

func TestCreateChatMessage_ChatNotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	msg := models.ChatMessage{
		ChatID:  uuid.New(),
		Message: "hi",
		Origin:  models.MessageOriginUser,
	}

	_, err := ds.CreateChatMessage(ctx, userID, msg)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestCreateChatMessage_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	msg := models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginUser,
	}

	_, err := ds.CreateChatMessage(ctx, otherID, msg)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestGetChatMessage_Found(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginUser,
	})
	require.NoError(t, err)

	got, err := ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "hi", got.Message)
}

func TestGetChatMessage_NotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	_, err := ds.GetChatMessage(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestGetChatMessage_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	created, err := ds.CreateChatMessage(ctx, ownerID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginUser,
	})
	require.NoError(t, err)

	_, err = ds.GetChatMessage(ctx, otherID, created.ID)
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestUpdateChatMessage_HappyPath(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginUser,
		Tokens:  1,
	})
	require.NoError(t, err)

	responseID := "resp-123"
	updated, err := ds.UpdateChatMessage(ctx, userID, models.ChatMessage{
		ID:         created.ID,
		Tokens:     99,
		ResponseID: &responseID,
		AdditionalContext: []models.AdditionalContextItem{
			{Type: models.AdditionalContextTypeMemory, Content: "new context"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(99), updated.Tokens)
	require.Equal(t, &responseID, updated.ResponseID)
	require.Len(t, updated.AdditionalContext, 1)
	require.Equal(t, "new context", updated.AdditionalContext[0].Content)

	// Reloading should reflect the same replaced context items (not appended).
	reloaded, err := ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.AdditionalContext, 1)
}

func TestUpdateChatMessage_NotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	_, err := ds.UpdateChatMessage(ctx, userID, models.ChatMessage{ID: uuid.New()})
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestUpdateChatMessage_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	created, err := ds.CreateChatMessage(ctx, ownerID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginUser,
	})
	require.NoError(t, err)

	_, err = ds.UpdateChatMessage(ctx, otherID, models.ChatMessage{ID: created.ID})
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestSetChatMessageLastError_SetAndClear(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	msg := "  provider timed out  "
	err = ds.SetChatMessageLastError(ctx, userID, created.ID, &msg)
	require.NoError(t, err)

	got, err := ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastErrorMessage)
	require.Equal(t, "provider timed out", *got.LastErrorMessage)

	// Passing nil clears it.
	err = ds.SetChatMessageLastError(ctx, userID, created.ID, nil)
	require.NoError(t, err)

	got, err = ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Nil(t, got.LastErrorMessage)
}

func TestSetChatMessageLastError_BlankMessageClears(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	blank := "   "
	err = ds.SetChatMessageLastError(ctx, userID, created.ID, &blank)
	require.NoError(t, err)

	got, err := ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Nil(t, got.LastErrorMessage)
}

func TestSetChatMessageLastError_NotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	msg := "boom"

	err := ds.SetChatMessageLastError(ctx, userID, uuid.New(), &msg)
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestSetChatMessageLastError_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	created, err := ds.CreateChatMessage(ctx, ownerID, models.ChatMessage{
		ChatID:  chatID,
		Message: "hi",
		Origin:  models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	msg := "nope"
	err = ds.SetChatMessageLastError(ctx, otherID, created.ID, &msg)
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestMarkChatMessagesRead_SqliteHappyPathAndNoop(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	// Two assistant (unread) messages and one user (already read) message.
	for i := 0; i < 2; i++ {
		_, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
			ChatID: chatID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
		})
		require.NoError(t, err)
	}
	_, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "user turn", Origin: models.MessageOriginUser,
	})
	require.NoError(t, err)

	count, err := ds.MarkChatMessagesRead(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Calling again is a no-op: nothing left unread.
	count, err = ds.MarkChatMessagesRead(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestMarkChatMessagesRead_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	_, err := ds.MarkChatMessagesRead(ctx, otherID, chatID)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestGetChatMessageCount_OriginFilters(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	_, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "u1", Origin: models.MessageOriginUser,
	})
	require.NoError(t, err)
	_, err = ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "a1", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)
	_, err = ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "a2", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	tests := []struct {
		name   string
		filter models.MessageOriginFilter
		want   int
	}{
		{name: "empty defaults to all", filter: "", want: 3},
		{name: "all", filter: models.MessageOriginFilterAll, want: 3},
		{name: "user only", filter: models.MessageOriginFilterUser, want: 1},
		{name: "assistant only", filter: models.MessageOriginFilterAssistant, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := ds.GetChatMessageCount(ctx, userID, chatID, tt.filter)
			require.NoError(t, err)
			require.Equal(t, tt.want, count)
		})
	}
}

func TestGetChatMessageCount_InvalidFilter(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	_, err := ds.GetChatMessageCount(ctx, userID, chatID, models.MessageOriginFilter("BOGUS"))
	require.ErrorIs(t, err, ErrInvalidMessageOriginFilter)
}

func TestGetChatMessageCount_ChatNotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	_, err := ds.GetChatMessageCount(ctx, userID, uuid.New(), models.MessageOriginFilterAll)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestSetChatMessageCheckpointCompletedAt_HappyPath(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	completedAt := time.Now().Add(-time.Minute)
	err = ds.SetChatMessageCheckpointCompletedAt(ctx, userID, created.ID, completedAt)
	require.NoError(t, err)

	got, err := ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.CheckpointCompletedAt)
	require.WithinDuration(t, completedAt, *got.CheckpointCompletedAt, time.Second)
}

func TestSetChatMessageCheckpointCompletedAt_NotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	err := ds.SetChatMessageCheckpointCompletedAt(ctx, userID, uuid.New(), time.Now())
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestSetChatMessageCheckpointCompletedAt_WrongOriginNotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	// A user-origin message doesn't match the OriginEQ(Assistant) predicate, so it's
	// indistinguishable from "not found" to this call.
	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "user turn", Origin: models.MessageOriginUser,
	})
	require.NoError(t, err)

	err = ds.SetChatMessageCheckpointCompletedAt(ctx, userID, created.ID, time.Now())
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestSetChatMessageCheckpointCompletedAt_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	created, err := ds.CreateChatMessage(ctx, ownerID, models.ChatMessage{
		ChatID: chatID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	err = ds.SetChatMessageCheckpointCompletedAt(ctx, otherID, created.ID, time.Now())
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestSetChatMessageContextBreakdown_HappyPath(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	breakdown := &modeltypes.ContextBreakdown{
		Version:      modeltypes.ContextBreakdownVersion,
		Segments:     []modeltypes.ContextSegmentStat{{Kind: "system_prompt", Segments: 1, Tokens: 100, Cacheable: true}},
		TotalTokens:  100,
		BudgetTokens: 8000,
		Model:        "gpt-test",
		Provider:     "openai",
		CapturedAt:   time.Now().UTC(),
	}

	err = ds.SetChatMessageContextBreakdown(ctx, userID, created.ID, breakdown)
	require.NoError(t, err)

	got, err := ds.GetChatMessage(ctx, userID, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ContextBreakdown)
	require.Equal(t, 100, got.ContextBreakdown.TotalTokens)
	require.Len(t, got.ContextBreakdown.Segments, 1)
}

// TestSetChatMessageContextBreakdown_NilOrEmptyIsNoop documents the best-effort contract:
// a nil breakdown, or one with no segments, is silently ignored rather than erroring — even
// when the target message doesn't exist.
func TestSetChatMessageContextBreakdown_NilOrEmptyIsNoop(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	err := ds.SetChatMessageContextBreakdown(ctx, userID, uuid.New(), nil)
	require.NoError(t, err)

	err = ds.SetChatMessageContextBreakdown(ctx, userID, uuid.New(), &modeltypes.ContextBreakdown{})
	require.NoError(t, err)
}

func TestSetChatMessageContextBreakdown_NotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	breakdown := &modeltypes.ContextBreakdown{Segments: []modeltypes.ContextSegmentStat{{Kind: "x", Segments: 1, Tokens: 1}}}

	err := ds.SetChatMessageContextBreakdown(ctx, userID, uuid.New(), breakdown)
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestSetChatMessageContextBreakdown_WrongOwner(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createCMTestUser(t, ds)
	otherID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, ownerID, modelID)

	created, err := ds.CreateChatMessage(ctx, ownerID, models.ChatMessage{
		ChatID: chatID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	breakdown := &modeltypes.ContextBreakdown{Segments: []modeltypes.ContextSegmentStat{{Kind: "x", Segments: 1, Tokens: 1}}}
	err = ds.SetChatMessageContextBreakdown(ctx, otherID, created.ID, breakdown)
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

// createCMTestMessages creates n user-origin messages with strictly increasing SentAt
// timestamps (oldest first), returning their IDs in creation order.
func createCMTestMessages(t *testing.T, ds *Datastore, userID, chatID uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	ids := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
			ChatID:  chatID,
			Message: "message",
			Origin:  models.MessageOriginUser,
			SentAt:  base.Add(time.Duration(i) * time.Second),
		})
		require.NoError(t, err)
		ids[i] = created.ID
	}
	return ids
}

func TestListChatMessages_NewestFirstAndPagination(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)
	ids := createCMTestMessages(t, ds, userID, chatID, 3)

	page, err := ds.ListChatMessages(ctx, userID, chatID, 1, 2, models.ChatMessageFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, page.TotalCount)
	require.Len(t, page.Results, 2)
	// Newest first: message[2] then message[1].
	require.Equal(t, ids[2], page.Results[0].(*models.ChatMessage).ID)
	require.Equal(t, ids[1], page.Results[1].(*models.ChatMessage).ID)

	page2, err := ds.ListChatMessages(ctx, userID, chatID, 2, 2, models.ChatMessageFilters{})
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)
	require.Equal(t, ids[0], page2.Results[0].(*models.ChatMessage).ID)
}

func TestListChatMessages_Filters(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	now := time.Now().UTC()
	_, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "hello world", Origin: models.MessageOriginUser, SentAt: now,
	})
	require.NoError(t, err)
	_, err = ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "goodbye", Origin: models.MessageOriginAssistant, SentAt: now.Add(time.Second),
	})
	require.NoError(t, err)

	origin := models.MessageOriginAssistant
	page, err := ds.ListChatMessages(ctx, userID, chatID, 1, 10, models.ChatMessageFilters{Origin: &origin})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	require.Equal(t, "goodbye", page.Results[0].(*models.ChatMessage).Message)

	query := "world"
	page, err = ds.ListChatMessages(ctx, userID, chatID, 1, 10, models.ChatMessageFilters{Query: &query})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	require.Equal(t, "hello world", page.Results[0].(*models.ChatMessage).Message)

	minDate := now.Add(500 * time.Millisecond)
	page, err = ds.ListChatMessages(ctx, userID, chatID, 1, 10, models.ChatMessageFilters{MinDate: &minDate})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	require.Equal(t, "goodbye", page.Results[0].(*models.ChatMessage).Message)
}

func TestListChatMessages_ChatNotFound(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)

	_, err := ds.ListChatMessages(ctx, userID, uuid.New(), 1, 10, models.ChatMessageFilters{})
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestListChatMessagesAfter_ChronologicalAndCursor(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)
	ids := createCMTestMessages(t, ds, userID, chatID, 3)

	// From the beginning: ascending order, oldest first.
	page, err := ds.ListChatMessagesAfter(ctx, userID, chatID, time.Time{}, uuid.Nil, 2, models.ChatMessageFilters{})
	require.NoError(t, err)
	require.Len(t, page.Results, 2)
	require.Equal(t, ids[0], page.Results[0].(*models.ChatMessage).ID)
	require.Equal(t, ids[1], page.Results[1].(*models.ChatMessage).ID)

	// Cursor past the first message returns the rest.
	first := page.Results[0].(*models.ChatMessage)
	page2, err := ds.ListChatMessagesAfter(ctx, userID, chatID, first.SentAt, first.ID, 10, models.ChatMessageFilters{})
	require.NoError(t, err)
	require.Len(t, page2.Results, 2)
	require.Equal(t, ids[1], page2.Results[0].(*models.ChatMessage).ID)
	require.Equal(t, ids[2], page2.Results[1].(*models.ChatMessage).ID)
}

func TestListChatMessagesAfter_CursorRequiresBothSentAtAndID(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	_, err := ds.ListChatMessagesAfter(ctx, userID, chatID, time.Now(), uuid.Nil, 10, models.ChatMessageFilters{})
	require.Error(t, err)

	_, err = ds.ListChatMessagesAfter(ctx, userID, chatID, time.Time{}, uuid.New(), 10, models.ChatMessageFilters{})
	require.Error(t, err)
}

func TestListChatMessagesAfter_PageSizeTooLarge(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	_, err := ds.ListChatMessagesAfter(ctx, userID, chatID, time.Time{}, uuid.Nil, maxChronologicalMessagePageSize+1, models.ChatMessageFilters{})
	require.Error(t, err)
}

func TestUpdateChatMessageGenerationExpression_HappyPath(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)

	now := time.Now().UTC()
	personality, err := ds.dbClient.Personality.Create().
		SetName("Vix").SetSystemPrompt("sp").SetUserID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	expression, err := ds.dbClient.PersonalityExpression.Create().
		SetExpressionKey("curious").
		SetPersonalityID(personality.ID).
		SetCreatedAt(now).SetUpdatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	chat, err := ds.dbClient.Chat.Create().
		SetName("Chat").SetOwnerID(userID).SetModelID(modelID).SetPersonalityID(personality.ID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chat.ID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	updated, err := ds.UpdateChatMessageGenerationExpression(ctx, userID, created.ID, &expression.ID, "  because curious  ")
	require.NoError(t, err)
	require.NotNil(t, updated.GenerationExpressionKey)
	require.Equal(t, "curious", *updated.GenerationExpressionKey)
	require.NotNil(t, updated.GenerationExpressionReasoning)
	require.Equal(t, "because curious", *updated.GenerationExpressionReasoning)

	// Clearing (nil expressionID) also clears reasoning.
	cleared, err := ds.UpdateChatMessageGenerationExpression(ctx, userID, created.ID, nil, "")
	require.NoError(t, err)
	require.Nil(t, cleared.GenerationExpressionKey)
	require.Nil(t, cleared.GenerationExpressionReasoning)
}

func TestUpdateChatMessageGenerationExpression_PersonalityMismatch(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)

	now := time.Now().UTC()
	chatPersonality, err := ds.dbClient.Personality.Create().
		SetName("Vix").SetSystemPrompt("sp").SetUserID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	otherPersonality, err := ds.dbClient.Personality.Create().
		SetName("Other").SetSystemPrompt("sp").SetUserID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	// Expression belongs to a different personality than the chat's.
	expression, err := ds.dbClient.PersonalityExpression.Create().
		SetExpressionKey("curious").
		SetPersonalityID(otherPersonality.ID).
		SetCreatedAt(now).SetUpdatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	chat, err := ds.dbClient.Chat.Create().
		SetName("Chat").SetOwnerID(userID).SetModelID(modelID).SetPersonalityID(chatPersonality.ID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chat.ID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	_, err = ds.UpdateChatMessageGenerationExpression(ctx, userID, created.ID, &expression.ID, "")
	require.ErrorIs(t, err, ErrGenerationExpressionPersonalityMismatch)
}

func TestUpdateChatMessageGenerationExpression_ChatHasNoPersonality(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID) // no personality set

	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "assistant reply", Origin: models.MessageOriginAssistant,
	})
	require.NoError(t, err)

	expressionID := uuid.New()
	_, err = ds.UpdateChatMessageGenerationExpression(ctx, userID, created.ID, &expressionID, "")
	require.ErrorIs(t, err, ErrGenerationExpressionPersonalityMismatch)
}

func TestUpdateChatMessageGenerationExpression_NotFoundOrNotAssistant(t *testing.T) {
	ds, cleanup := newChatMessageTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createCMTestUser(t, ds)
	modelID := createCMTestModel(t, ds)
	chatID := createCMTestChat(t, ds, userID, modelID)

	_, err := ds.UpdateChatMessageGenerationExpression(ctx, userID, uuid.New(), nil, "")
	require.ErrorIs(t, err, ErrChatMessageNotFound)

	// A user-origin message doesn't match the assistant-only predicate.
	created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID: chatID, Message: "user turn", Origin: models.MessageOriginUser,
	})
	require.NoError(t, err)

	_, err = ds.UpdateChatMessageGenerationExpression(ctx, userID, created.ID, nil, "")
	require.ErrorIs(t, err, ErrChatMessageNotFound)
}

func TestTrimChatMessageLastError(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty string", in: "", want: ""},
		{name: "whitespace only", in: "  \n ", want: ""},
		{name: "trims surrounding whitespace", in: "  boom  ", want: "boom"},
		{name: "short string unchanged", in: "connection reset", want: "connection reset"},
		{name: "exactly at max runes", in: strings.Repeat("e", 4096), want: strings.Repeat("e", 4096)},
		{name: "over max runes is truncated without re-trimming", in: strings.Repeat("e", 4096) + "   tail", want: strings.Repeat("e", 4096)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimChatMessageLastError(tt.in)
			require.Equal(t, tt.want, got)
			require.LessOrEqual(t, len([]rune(got)), 4096)
		})
	}
}
