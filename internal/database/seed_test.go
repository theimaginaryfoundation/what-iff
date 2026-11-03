package database

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
)

func newMockClient(t *testing.T) (*ent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

// expectExistingRoles answers ensureRoles with roles that already exist, so no
// role is created: a lookup of each inside its transaction, then a re-read of
// each after the commit.
func expectExistingRoles(mock sqlmock.Sqlmock) {
	role := func(name string) {
		mock.ExpectQuery(`SELECT .* FROM "roles"`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(uuid.New(), name))
	}
	mock.ExpectBegin()
	role("user")
	role("admin")
	mock.ExpectCommit()
	role("user")
	role("admin")
}

// Boot's seed step creates the roles and reads no model rows at all: seeding
// the catalog and requiring its default are separate steps (SeedCatalog,
// RequireCatalogDefault) that cmd/api-server calls only where they apply.
// sqlmock fails any query it was not told to expect.
func TestEnsureSeedData_TouchesNoModels(t *testing.T) {
	client, mock := newMockClient(t)

	expectExistingRoles(mock)

	require.NoError(t, EnsureSeedData(context.Background(), client, zap.NewNop()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// A deployment that curates its catalog must fail fast without its default.
func TestRequireCatalogDefault(t *testing.T) {
	client, mock := newMockClient(t)
	mock.ExpectQuery(`SELECT .* FROM "models"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(uuid.New(), "gpt-5.1"))
	require.NoError(t, RequireCatalogDefault(context.Background(), client))

	mock.ExpectQuery(`SELECT .* FROM "models"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
	require.ErrorContains(t, RequireCatalogDefault(context.Background(), client), "default model not found in the catalog")
	require.NoError(t, mock.ExpectationsWereMet())
}

// A real backend seeds the catalog only where the build asks.
func TestSeedModelCatalog_OffByDefault(t *testing.T) {
	require.False(t, SeedModelCatalog())
}
