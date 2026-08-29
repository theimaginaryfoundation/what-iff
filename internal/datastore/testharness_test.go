package datastore

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"
)

func ensureChatMessageBookmarkedTestColumn(t *testing.T, db *sql.DB) {
	t.Helper()

	var tableName string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'chat_messages'`).Scan(&tableName)
	if err == sql.ErrNoRows {
		return
	}
	require.NoError(t, err)

	rows, err := db.Query(`PRAGMA table_info(chat_messages)`)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue any
		require.NoError(t, rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk))
		if name == "bookmarked" {
			return
		}
	}
	require.NoError(t, rows.Err())

	_, err = db.Exec(`ALTER TABLE chat_messages ADD COLUMN bookmarked bool NOT NULL DEFAULT false`)
	require.NoError(t, err)
}

// newTestDatastore opens an in-memory sqlite DB, applies the given schema-creation funcs in
// order, and wraps it in a Datastore. Schema funcs like createMemoryImportTestSchema take
// dependencies as ordering (parent tables before child FKs) — see each func's doc comment.
func newTestDatastore(t *testing.T, schemas ...func(*testing.T, *sql.DB)) (*Datastore, func()) {
	t.Helper()

	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)

	for _, schema := range schemas {
		schema(t, db)
	}
	ensureChatMessageBookmarkedTestColumn(t, db)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	cleanup := func() {
		_ = client.Close()
		_ = db.Close()
	}
	return ds, cleanup
}
