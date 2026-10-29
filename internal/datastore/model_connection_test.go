package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createConnectionModelsTestSchema creates the tables a connection's models
// touch: users, provider connections and models (ADR 0x027).
func createConnectionModelsTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE users (id uuid PRIMARY KEY)`,
		`CREATE TABLE provider_connections (
			id uuid PRIMARY KEY, created_at datetime NOT NULL, updated_at datetime NOT NULL,
			user_id uuid NULL REFERENCES users(id) ON DELETE CASCADE,
			preset text NOT NULL, flavor text NOT NULL DEFAULT '', name text NOT NULL,
			base_url text NOT NULL DEFAULT '', key text NOT NULL DEFAULT '', hint text NOT NULL DEFAULT '',
			source text NOT NULL DEFAULT '', builtin text NULL
		)`,
		`CREATE TABLE models (
			id uuid PRIMARY KEY, name text NOT NULL, display_name text NOT NULL, description text NOT NULL,
			provider text NOT NULL DEFAULT 'openai', tool_support bool NOT NULL DEFAULT false,
			reasoning_support_override text NULL, vision_support bool NULL,
			base_credits_per_slab integer NOT NULL DEFAULT 1, subscription_tier text NOT NULL DEFAULT 'high',
			deleted bool NOT NULL DEFAULT false, is_default bool NOT NULL DEFAULT false,
			connection_id uuid NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
			UNIQUE (connection_id, name)
		)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

type connectionModelsFixture struct {
	ds                *Datastore
	alice, bob        uuid.UUID
	catalog, aliceOwn uuid.UUID
}

func newConnectionModelsFixture(t *testing.T) connectionModelsFixture {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createConnectionModelsTestSchema)
	t.Cleanup(cleanup)
	ctx := context.Background()
	f := connectionModelsFixture{ds: ds, alice: uuid.New(), bob: uuid.New()}
	for _, id := range []uuid.UUID{f.alice, f.bob} {
		_, err := ds.sqlDB.ExecContext(ctx, `INSERT INTO users (id) VALUES (?)`, id)
		require.NoError(t, err)
	}
	conn, err := ds.dbClient.ProviderConnection.Create().
		SetUserID(f.alice).SetPreset("ollama").SetName("Ollama").
		Save(ctx)
	require.NoError(t, err)
	catalog, err := ds.dbClient.Model.Create().
		SetName("gpt-5.6-luna").SetDisplayName("GPT-5.6 Luna").SetDescription("").
		Save(ctx)
	require.NoError(t, err)
	own, err := ds.dbClient.Model.Create().
		SetName("qwen3:4b").SetDisplayName("qwen3:4b").SetDescription("").
		SetProvider("ollama").SetConnectionID(conn.ID).
		Save(ctx)
	require.NoError(t, err)
	f.catalog, f.aliceOwn = catalog.ID, own.ID
	return f
}

// The catalog lists only the deployment's own models; an account's connection
// models are its own.
func TestListModels_IsTheCatalogOnly(t *testing.T) {
	f := newConnectionModelsFixture(t)
	all, err := f.ds.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, f.catalog, all[0].ID)
	require.Nil(t, all[0].ConnectionID)
}

func TestConnectionModels_AreTheOwnersOnly(t *testing.T) {
	f := newConnectionModelsFixture(t)
	own, err := f.ds.connectionModels(context.Background(), f.alice)
	require.NoError(t, err)
	require.Len(t, own, 1)
	require.Equal(t, f.aliceOwn, own[0].ID)
	require.NotNil(t, own[0].ConnectionID)

	others, err := f.ds.connectionModels(context.Background(), f.bob)
	require.NoError(t, err)
	require.Empty(t, others)
}

// Another account's connection model does not exist to the usage check, so it
// can't be set on a chat, job or preference.
func TestAssertUserCanUseModelTx_ConnectionModelsOnlyForTheirOwner(t *testing.T) {
	f := newConnectionModelsFixture(t)
	ctx := context.Background()
	tx, err := f.ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	require.NoError(t, assertUserCanUseModelTx(ctx, tx, f.alice, f.aliceOwn))
	err = assertUserCanUseModelTx(ctx, tx, f.bob, f.aliceOwn)
	require.True(t, errors.Is(err, ErrModelNotFound), "got %v", err)
}

// An account may enable a model with a catalog model's name on its own
// connection. Lookups by name that mean the catalog must find the catalog's
// one, and the catalog's own name checks must not see the account's.
func TestCatalogNameLookups_IgnoreConnectionModels(t *testing.T) {
	f := newConnectionModelsFixture(t)
	ctx := context.Background()
	conn, err := f.ds.dbClient.ProviderConnection.Create().
		SetUserID(f.alice).SetPreset("openai").SetBuiltin("openai").SetName("OpenAI").
		Save(ctx)
	require.NoError(t, err)
	_, err = f.ds.dbClient.Model.Create().
		SetName("gpt-5.6-luna").SetDisplayName("mine").SetDescription("").
		SetProvider("openai").SetConnectionID(conn.ID).
		Save(ctx)
	require.NoError(t, err)

	got, err := f.ds.GetModelByName(ctx, "gpt-5.6-luna")
	require.NoError(t, err, "not ambiguous with the account's model")
	require.Equal(t, f.catalog, got.ID)

	t.Setenv("DEFAULT_MODEL_NAME", "gpt-5.6-luna")
	tx, err := f.ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	def, err := resolveDefaultModelTx(ctx, tx)
	require.NoError(t, err, "a new account's default is found, so signup works")
	require.Equal(t, f.catalog, def.ID)
	require.NoError(t, tx.Rollback())

	created, err := f.ds.AdminCreateModel(ctx, models.CreateModelRequest{Name: "qwen3:4b", DisplayName: "Qwen", Provider: "qwen"})
	require.NoError(t, err, "an account's model of that name is not the catalog's")
	require.Nil(t, created.ConnectionID)
	newName := "gpt-5.6-luna"
	_, err = f.ds.AdminUpdateModel(ctx, created.ID, models.UpdateModelRequest{Name: newName})
	require.Error(t, err, "renaming onto a catalog model's name is still refused")
}
