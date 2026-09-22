package datastore

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// ensureChatForkTestColumns adds the branch-lineage columns (ent/migrate/schema.go's
// ChatsColumns forked_from_*) to any hand-written test chats table that predates them. ent selects
// every column explicitly, so without this every chat query in those suites would fail.
func ensureChatForkTestColumns(t *testing.T, db *sql.DB) {
	t.Helper()

	var tableName string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'chats'`).Scan(&tableName)
	if err == sql.ErrNoRows {
		return
	}
	require.NoError(t, err)

	rows, err := db.Query(`PRAGMA table_info(chats)`)
	require.NoError(t, err)
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue any
		require.NoError(t, rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk))
		have[name] = true
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	for _, col := range []string{"forked_from_chat_id", "forked_from_message_id"} {
		if have[col] {
			continue
		}
		_, err := db.Exec(`ALTER TABLE chats ADD COLUMN ` + col + ` uuid`)
		require.NoError(t, err)
	}
}
