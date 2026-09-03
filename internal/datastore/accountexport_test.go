package datastore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
)

func TestExportConversationInputsUsesSentAtIDCursor(t *testing.T) {
	ds, cleanup := newSQLiteDatastore(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	// The shared SQLite fixture intentionally omits messages, so add the
	// production columns required by Ent's export query for this regression.
	_, err := ds.sqlDB.Exec(`
		ALTER TABLE chats ADD COLUMN source text;
		ALTER TABLE chats ADD COLUMN import_hash text;
		ALTER TABLE chats ADD COLUMN rehydration_state text;
		CREATE TABLE chat_messages (
			id uuid PRIMARY KEY,
			message text NOT NULL,
			origin text NOT NULL,
			read_status text NOT NULL,
			response_id text,
			sent_at datetime NOT NULL,
			tokens integer,
			generation_model text,
			generation_personality text,
			generation_expression_reasoning text,
			last_error_message text,
			checkpoint_completed_at datetime,
			chat_messages uuid NOT NULL,
			chat_message_generation_mood uuid,
			chat_message_generation_expression uuid
		)`)
	require.NoError(t, err)

	owner, err := ds.dbClient.User.Create().
		SetUsername("export-owner").
		SetEmail("export-owner@example.com").
		SetPasswordHash("password-hash").
		Save(ctx)
	require.NoError(t, err)
	chat, err := ds.dbClient.Chat.Create().
		SetName("Export chat").
		SetOwnerID(owner.ID).
		Save(ctx)
	require.NoError(t, err)

	sentAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	for i := range accountExportBatchSize + 1 {
		_, err := ds.dbClient.ChatMessage.Create().
			SetChatID(chat.ID).
			SetMessage(fmt.Sprintf("message-%03d", i)).
			SetOrigin(chatmessage.OriginUser).
			SetSentAt(sentAt).
			Save(ctx)
		require.NoError(t, err)
	}

	conversations, err := ds.ExportConversationInputs(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, conversations, 1)
	require.Len(t, conversations[0].Messages, accountExportBatchSize+1)

	seen := make(map[string]bool, len(conversations[0].Messages))
	for _, message := range conversations[0].Messages {
		seen[message.Text] = true
	}
	for i := range accountExportBatchSize + 1 {
		require.Truef(t, seen[fmt.Sprintf("message-%03d", i)], "message %d was skipped", i)
	}
}
