package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// createProviderKeyTestSchema creates users and user_provider_keys. Users must
// exist first: the owner edge is a required FK.
func createProviderKeyTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id uuid PRIMARY KEY,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		username varchar NOT NULL,
		email varchar NOT NULL,
		password_hash varchar NOT NULL,
		timezone varchar NOT NULL DEFAULT 'America/New_York',
		status varchar NOT NULL DEFAULT 'active',
		enable_experimental_models bool NOT NULL DEFAULT false
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE user_provider_keys (
		id uuid PRIMARY KEY,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		provider varchar(40) NOT NULL,
		encrypted_key varchar NOT NULL,
		key_hint varchar(12),
		user_provider_keys uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE UNIQUE INDEX userproviderkey_provider_user_provider_keys
		ON user_provider_keys (provider, user_provider_keys)`)
	require.NoError(t, err)
}

func seedProviderKeyUser(t *testing.T, ds *Datastore, username string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.sqlDB.Exec(
		`INSERT INTO users (id, created_at, updated_at, username, email, password_hash)
		 VALUES (?, datetime('now'), datetime('now'), ?, ?, 'x')`,
		id, username, username+"@example.test")
	require.NoError(t, err)
	return id
}

// The key must survive a round trip through encryption, and the stored value
// must not be the plaintext — it is written to a database the operator can read.
func TestUserProviderKeyRoundTripsAndIsEncryptedAtRest(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createProviderKeyTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedProviderKeyUser(t, ds, "alice")

	info, err := ds.SetUserProviderKey(ctx, userID, "openai", "sk-alice-secret-value")
	require.NoError(t, err)
	require.Equal(t, "openai", info.Provider)
	require.Equal(t, "…alue", info.KeyHint)

	got, err := ds.GetUserProviderKey(ctx, userID, "openai")
	require.NoError(t, err)
	require.Equal(t, "sk-alice-secret-value", got)

	var stored string
	require.NoError(t, ds.sqlDB.QueryRow(`SELECT encrypted_key FROM user_provider_keys`).Scan(&stored))
	require.NotContains(t, stored, "sk-alice-secret-value")
}

// Two accounts on one instance must never see each other's credential — the
// reason keys are per-account rather than per-deployment.
func TestUserProviderKeysAreIsolatedPerAccount(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createProviderKeyTestSchema)
	defer cleanup()
	ctx := context.Background()
	alice := seedProviderKeyUser(t, ds, "alice")
	bob := seedProviderKeyUser(t, ds, "bob")

	_, err := ds.SetUserProviderKey(ctx, alice, "openai", "sk-alice-0000")
	require.NoError(t, err)
	_, err = ds.SetUserProviderKey(ctx, bob, "openai", "sk-bob-1111")
	require.NoError(t, err)

	got, err := ds.GetUserProviderKey(ctx, alice, "openai")
	require.NoError(t, err)
	require.Equal(t, "sk-alice-0000", got)

	got, err = ds.GetUserProviderKey(ctx, bob, "openai")
	require.NoError(t, err)
	require.Equal(t, "sk-bob-1111", got)
}

// Setting a key for a provider replaces the previous one rather than adding a
// second row nothing would choose between.
func TestSetUserProviderKeyReplaces(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createProviderKeyTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedProviderKeyUser(t, ds, "alice")

	_, err := ds.SetUserProviderKey(ctx, userID, "openai", "sk-first-0000")
	require.NoError(t, err)
	_, err = ds.SetUserProviderKey(ctx, userID, "openai", "sk-second-1111")
	require.NoError(t, err)

	got, err := ds.GetUserProviderKey(ctx, userID, "openai")
	require.NoError(t, err)
	require.Equal(t, "sk-second-1111", got)

	var rows int
	require.NoError(t, ds.sqlDB.QueryRow(`SELECT count(*) FROM user_provider_keys`).Scan(&rows))
	require.Equal(t, 1, rows)
}

func TestGetUserProviderKeyMissingIsTyped(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createProviderKeyTestSchema)
	defer cleanup()
	userID := seedProviderKeyUser(t, ds, "alice")

	_, err := ds.GetUserProviderKey(context.Background(), userID, "openai")
	require.True(t, errors.Is(err, ErrProviderKeyNotFound), "want ErrProviderKeyNotFound, got %v", err)
}

// Listing is for display, so it carries hints and never the key itself.
func TestListUserProviderKeysOmitsTheSecret(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createProviderKeyTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedProviderKeyUser(t, ds, "alice")

	_, err := ds.SetUserProviderKey(ctx, userID, "openai", "sk-alice-abcd")
	require.NoError(t, err)
	_, err = ds.SetUserProviderKey(ctx, userID, "anthropic", "sk-ant-wxyz")
	require.NoError(t, err)

	list, err := ds.ListUserProviderKeys(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "anthropic", list[0].Provider)
	require.Equal(t, "…wxyz", list[0].KeyHint)
	require.Equal(t, "openai", list[1].Provider)
	require.Equal(t, "…abcd", list[1].KeyHint)
}

// Deleting something that is not there satisfies the caller's intent.
func TestDeleteUserProviderKeyIsIdempotent(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createProviderKeyTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedProviderKeyUser(t, ds, "alice")

	require.NoError(t, ds.DeleteUserProviderKey(ctx, userID, "openai"))
	_, err := ds.SetUserProviderKey(ctx, userID, "openai", "sk-alice-abcd")
	require.NoError(t, err)
	require.NoError(t, ds.DeleteUserProviderKey(ctx, userID, "openai"))
	_, err = ds.GetUserProviderKey(ctx, userID, "openai")
	require.True(t, errors.Is(err, ErrProviderKeyNotFound))
}
