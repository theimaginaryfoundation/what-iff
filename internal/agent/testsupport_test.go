package agent

import (
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// newTestDatastore builds a Datastore backed by go-sqlmock for unit tests that
// exercise the agent's datastore interactions without a real database.
func newTestDatastore(t *testing.T) (*datastore.Datastore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)
	return ds, mock, func() {
		_ = client.Close()
	}
}

// newTestAgent builds a minimal Agent wired to ds for tests that only touch
// datastore-backed helpers (no meter/provider dependencies).
func newTestAgent(ds *datastore.Datastore) *Agent {
	return &Agent{ds: ds, logger: zap.NewNop()}
}
