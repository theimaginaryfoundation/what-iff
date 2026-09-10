package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// importDedupSchema is a minimal users+chats schema sufficient for exercising the ImportChats
// dedup query. The skip paths return before any chat/message is created, so no chat_messages
// table (or the full chats column set) is required. It is intentionally minimal and must stay in
// sync with the columns the dedup query touches (owner edge, import_hash, id); a create/import
// path would need the full ent-generated schema instead.
func importDedupSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE users (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			username text NOT NULL UNIQUE,
			email text NOT NULL UNIQUE,
			password_hash text NOT NULL
		)`,
		`CREATE TABLE chats (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			import_hash text,
			source text,
			user_chats uuid NOT NULL
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
}

// TestImportChatsSkipsNativeRoundTripAndPriorImport covers both dedup skip paths:
//   - a native chat (no import_hash) is matched by the export's source conversation id (SourceID),
//     so importing an account export back into its origin account does not duplicate the thread;
//   - a prior import is still matched by import_hash.
func TestImportChatsSkipsNativeRoundTripAndPriorImport(t *testing.T) {
	ds, cleanup := newTestDatastore(t, importDedupSchema)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	now := time.Now().UTC()
	_, err := ds.sqlDB.Exec(
		`INSERT INTO users (id, created_at, updated_at, username, email, password_hash)
		 VALUES (?, ?, ?, 'owner', 'owner@example.com', 'hash')`,
		userID, now, now)
	require.NoError(t, err)

	// A native chat: created in-app, so it has NO import_hash. Its id is what an account export
	// records as the conversation uuid, and thus what a round-trip import carries as SourceID.
	nativeChatID := uuid.New()
	_, err = ds.sqlDB.Exec(
		`INSERT INTO chats (id, created_at, updated_at, name, import_hash, user_chats)
		 VALUES (?, ?, ?, 'Native thread', NULL, ?)`,
		nativeChatID, now, now, userID)
	require.NoError(t, err)

	// A previously imported chat: carries an import_hash.
	priorHash := "prior-import-hash"
	_, err = ds.sqlDB.Exec(
		`INSERT INTO chats (id, created_at, updated_at, name, import_hash, user_chats)
		 VALUES (?, ?, ?, 'Prior import', ?, ?)`,
		uuid.New(), now, now, priorHash, userID)
	require.NoError(t, err)

	msg := models.ChatMessage{Message: "hello", Origin: models.MessageOriginUser, SentAt: now}
	convs := []models.ImportConversation{
		{
			// Round-trip of the native thread: import_hash would not match (native has none), but
			// SourceID equals the native chat's id, so it must be skipped.
			Title:      "Native thread",
			CreatedAt:  now,
			Source:     models.ChatSourceAnthropic,
			ImportHash: "fresh-hash-not-in-db",
			SourceID:   &nativeChatID,
			Messages:   []models.ChatMessage{msg},
		},
		{
			// Re-import of a prior import: matched by import_hash.
			Title:      "Prior import",
			CreatedAt:  now,
			Source:     models.ChatSourceAnthropic,
			ImportHash: priorHash,
			Messages:   []models.ChatMessage{msg},
		},
	}

	result, err := ds.ImportChats(ctx, userID, convs, nil)
	require.NoError(t, err)
	require.Equal(t, 0, result.Imported, "no conversation should be imported")
	require.Equal(t, 2, result.Skipped, "both the native round-trip and the prior import should be skipped")
	require.Empty(t, result.Errors)
}
