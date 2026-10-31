package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBackfillFileOrigins(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite3")
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE file_attachments (id TEXT PRIMARY KEY, agent_authored BOOL NOT NULL DEFAULT false, origin TEXT NOT NULL DEFAULT 'user')`)
	require.NoError(t, err)
	for _, row := range []struct {
		flag   bool
		origin string
	}{{true, "user"}, {true, "agent"}, {false, "user"}, {false, "skill"}} {
		_, err := db.Exec(`INSERT INTO file_attachments (id, agent_authored, origin) VALUES (?, ?, ?)`, uuid.NewString(), row.flag, row.origin)
		require.NoError(t, err)
	}

	n, err := BackfillFileOrigins(context.Background(), db, zap.NewNop())
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "only the flagged row still marked user changes")
	var agents int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM file_attachments WHERE origin = 'agent'`).Scan(&agents))
	require.Equal(t, 2, agents)

	n, err = BackfillFileOrigins(context.Background(), db, nil)
	require.NoError(t, err)
	require.Zero(t, n)

	// Without the legacy column there is nothing to do.
	_, err = db.Exec(`DROP TABLE file_attachments`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE file_attachments (id TEXT PRIMARY KEY, origin TEXT NOT NULL DEFAULT 'user')`)
	require.NoError(t, err)
	n, err = BackfillFileOrigins(context.Background(), db, nil)
	require.NoError(t, err)
	require.Zero(t, n)
}
