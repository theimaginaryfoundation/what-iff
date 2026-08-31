package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func addImportOrderRepairChatProvenanceTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`ALTER TABLE chats ADD COLUMN source text`)
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE chats ADD COLUMN import_hash text`)
	require.NoError(t, err)
}

func newImportOrderRepairTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, addImportOrderRepairChatProvenanceTestSchema)
}

func createImportOrderTestChat(t *testing.T, ds *Datastore, imported, archived bool, lastMessageTime time.Time) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	userID := createCMTestUser(t, ds)
	builder := ds.dbClient.Chat.Create().
		SetName("import-order-" + uuid.NewString()[:8]).
		SetOwnerID(userID).
		SetArchived(archived).
		SetCreatedAt(lastMessageTime.Add(-time.Hour)).
		SetUpdatedAt(lastMessageTime).
		SetLastMessageTime(lastMessageTime)
	if imported {
		builder.SetSource(models.ChatSourceOpenAI).
			SetImportHash(uuid.NewString())
	}
	chat, err := builder.Save(ctx)
	require.NoError(t, err)
	return userID, chat.ID
}

func createImportOrderTestMessage(t *testing.T, ds *Datastore, chatID uuid.UUID, origin models.MessageOrigin, sentAt time.Time) uuid.UUID {
	t.Helper()
	msg, err := ds.dbClient.ChatMessage.Create().
		SetMessage(string(origin)).
		SetOrigin(chatmessage.Origin(origin)).
		SetReadStatus(chatmessage.ReadStatusRead).
		SetSentAt(sentAt).
		SetChatID(chatID).
		Save(context.Background())
	require.NoError(t, err)
	return msg.ID
}

func messageTime(t *testing.T, ds *Datastore, id uuid.UUID) time.Time {
	t.Helper()
	msg, err := ds.dbClient.ChatMessage.Get(context.Background(), id)
	require.NoError(t, err)
	return msg.SentAt
}

func TestRepairImportedMessageOrderRepairsExactUserAssistantPair(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	base := time.Date(2026, 8, 31, 12, 0, 0, 123456000, time.UTC)
	_, chatID := createImportOrderTestChat(t, ds, true, true, base)
	userID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base)
	assistantID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base)

	result, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.RepairedPairs)
	require.Equal(t, base, messageTime(t, ds, userID))
	require.Equal(t, base.Add(time.Microsecond), messageTime(t, ds, assistantID))

	chat, err := ds.dbClient.Chat.Get(context.Background(), chatID)
	require.NoError(t, err)
	require.Equal(t, base.Add(time.Microsecond), *chat.LastMessageTime)
}

func TestRepairImportedMessageOrderAbstainsOnAmbiguousTimestampGroup(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	base := time.Date(2026, 8, 31, 12, 1, 0, 0, time.UTC)
	_, chatID := createImportOrderTestChat(t, ds, true, true, base)
	ids := []uuid.UUID{
		createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base),
		createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base),
		createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base),
		createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base),
	}

	result, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Zero(t, result.RepairedPairs)
	for _, id := range ids {
		require.Equal(t, base, messageTime(t, ds, id))
	}
}

func TestRepairImportedMessageOrderSkipsNativeChat(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	base := time.Date(2026, 8, 31, 12, 2, 0, 0, time.UTC)
	_, chatID := createImportOrderTestChat(t, ds, false, true, base)
	createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base)
	assistantID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base)

	result, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Zero(t, result.RepairedPairs)
	require.Equal(t, base, messageTime(t, ds, assistantID))
}

func TestRepairImportedMessageOrderSkipsUnarchivedImportedChat(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	base := time.Date(2026, 8, 31, 12, 3, 0, 0, time.UTC)
	_, chatID := createImportOrderTestChat(t, ds, true, false, base)
	createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base)
	assistantID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base)

	result, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Zero(t, result.RepairedPairs)
	require.Equal(t, base, messageTime(t, ds, assistantID))
}

func TestRepairImportedMessageOrderSkipsJobAssociatedImportedChat(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	base := time.Date(2026, 8, 31, 12, 4, 0, 0, time.UTC)
	userID, chatID := createImportOrderTestChat(t, ds, true, true, base)
	createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base)
	assistantID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base)

	_, err := ds.sqlDB.ExecContext(ctx, `INSERT INTO agent_jobs (
		id, created_at, updated_at, prompt, schedule_input, schedule_type, timezone, status, run_count,
		chat_agent_jobs, user_agent_jobs
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		uuid.New(), base, base, "test", "now", "one_time", "UTC", "active", 0, chatID, userID)
	require.NoError(t, err)

	result, err := ds.RepairImportedMessageOrder(ctx)
	require.NoError(t, err)
	require.Zero(t, result.RepairedPairs)
	require.Equal(t, base, messageTime(t, ds, assistantID))
}

func TestRepairImportedMessageOrderAbstainsWhenRepairWouldCollide(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	base := time.Date(2026, 8, 31, 12, 5, 0, 0, time.UTC)
	_, chatID := createImportOrderTestChat(t, ds, true, true, base.Add(time.Microsecond))
	createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base)
	assistantID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base)
	createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base.Add(time.Microsecond))

	result, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Zero(t, result.RepairedPairs)
	require.Equal(t, base, messageTime(t, ds, assistantID))
}

func TestRepairImportedMessageOrderIsIdempotent(t *testing.T) {
	ds, cleanup := newImportOrderRepairTestDatastore(t)
	defer cleanup()

	base := time.Date(2026, 8, 31, 12, 6, 0, 0, time.UTC)
	_, chatID := createImportOrderTestChat(t, ds, true, true, base)
	createImportOrderTestMessage(t, ds, chatID, models.MessageOriginUser, base)
	assistantID := createImportOrderTestMessage(t, ds, chatID, models.MessageOriginAssistant, base)

	first, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, first.RepairedPairs)
	firstTime := messageTime(t, ds, assistantID)

	second, err := ds.RepairImportedMessageOrder(context.Background())
	require.NoError(t, err)
	require.Zero(t, second.RepairedPairs)
	require.Equal(t, firstTime, messageTime(t, ds, assistantID))
}
