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

func withCatalogSeeding(t *testing.T, on bool) {
	t.Helper()
	prev := SeedModelCatalog
	SeedModelCatalog = func() bool { return on }
	t.Cleanup(func() { SeedModelCatalog = prev })
}

// With catalog seeding off, boot must not read or write a single catalog row:
// the only model query is the check that the default model exists. sqlmock
// fails any query it was not told to expect, so a catalog lookup or insert
// sneaking back in fails this test.
func TestEnsureSeedData_CatalogSeedingOffTouchesNoCatalogRows(t *testing.T) {
	withCatalogSeeding(t, false)
	client, mock := newMockClient(t)

	expectExistingRoles(mock)
	mock.ExpectQuery(`SELECT .* FROM "models"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(uuid.New(), "gpt-5.1"))

	require.NoError(t, EnsureSeedData(context.Background(), client, zap.NewNop()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// The default-model check still runs with seeding off: a build that curates
// its own catalog must still fail fast if the default model is missing.
func TestEnsureSeedData_CatalogSeedingOffStillRequiresTheDefaultModel(t *testing.T) {
	withCatalogSeeding(t, false)
	client, mock := newMockClient(t)

	expectExistingRoles(mock)
	mock.ExpectQuery(`SELECT .* FROM "models"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))

	err := EnsureSeedData(context.Background(), client, zap.NewNop())
	require.ErrorContains(t, err, "default model not found in the catalog")
	require.NoError(t, mock.ExpectationsWereMet())
}

// A seeded install (the default) no longer seeds, and has no catalog default
// to require: boot reads no model rows at all. Its seeded models are converted
// separately (ConvertSeededModels).
func TestEnsureSeedData_SeededInstallTouchesNoModels(t *testing.T) {
	withCatalogSeeding(t, true)
	client, mock := newMockClient(t)

	expectExistingRoles(mock)

	require.NoError(t, EnsureSeedData(context.Background(), client, zap.NewNop()))
	require.NoError(t, mock.ExpectationsWereMet())
}
