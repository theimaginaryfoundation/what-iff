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
