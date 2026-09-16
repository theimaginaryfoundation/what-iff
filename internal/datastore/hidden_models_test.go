package datastore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// hiddenModelsSchema adds user_preferences. Users come from
// createMemoryImportTestSchema, which already maintains the full column set —
// a second copy here would drift the next time a user column is added, which
// is how this test first failed.
func hiddenModelsSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		// Column names taken from ent/migrate/schema.go rather than guessed.
		`CREATE TABLE IF NOT EXISTS models (
			id uuid PRIMARY KEY, created_at datetime NOT NULL, updated_at datetime NOT NULL,
			name varchar NOT NULL, display_name varchar NOT NULL, description varchar NOT NULL,
			provider varchar NOT NULL DEFAULT 'openai', tool_support bool NOT NULL DEFAULT true,
			base_credits_per_slab bigint NOT NULL DEFAULT 0,
			subscription_tier varchar NOT NULL DEFAULT 'high',
			deleted bool NOT NULL DEFAULT false, is_default bool NOT NULL DEFAULT false)`,
		`CREATE TABLE user_preferences (
			id uuid PRIMARY KEY,
			theme varchar NOT NULL DEFAULT 'dark',
			last_seen_announcement varchar DEFAULT '',
			experimental_memory_dedupe_chain bool NOT NULL DEFAULT false,
			favorite_model_ids json NOT NULL DEFAULT '[]',
			hidden_model_ids json NOT NULL DEFAULT '[]',
			added_model_ids json NOT NULL DEFAULT '[]',
			seen_model_ids json NOT NULL DEFAULT '[]',
			user_preferences uuid UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			default_model uuid,
			default_personality uuid)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func seedHiddenTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.sqlDB.Exec(`INSERT INTO users (id, created_at, updated_at, username, email, password_hash)
		VALUES (?, datetime('now'), datetime('now'), 'u', 'u@example.test', 'x')`, id)
	require.NoError(t, err)
	// UpdateUserPreferences edits an existing row rather than creating one.
	defaultModel := uuid.New()
	_, err = ds.sqlDB.Exec(`INSERT INTO models (id, created_at, updated_at, name, display_name, description)
		VALUES (?, datetime('now'), datetime('now'), 'seed', 'Seed', 'seed model')`, defaultModel)
	require.NoError(t, err)
	_, err = ds.sqlDB.Exec(`INSERT INTO user_preferences (id, theme, favorite_model_ids, hidden_model_ids, user_preferences, default_model)
		VALUES (?, 'dark', '[]', '[]', ?, ?)`, uuid.New(), id, defaultModel)
	require.NoError(t, err)
	return id
}

func mdl(name string) *models.Model { return &models.Model{ID: uuid.New(), Name: name} }

// Hiding removes a model from the listing and leaves the rest alone.
func TestFilterHiddenModelsRemovesOnlyTheHidden(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, hiddenModelsSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedHiddenTestUser(t, ds)

	keep, drop := mdl("gpt-5.1"), mdl("gpt-4o")
	_, err := ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{
		HiddenModelIDs: []string{drop.ID.String()},
	})
	require.NoError(t, err)

	got := ds.filterHiddenModels(ctx, userID, []*models.Model{keep, drop})
	require.Len(t, got, 1)
	require.Equal(t, "gpt-5.1", got[0].Name)
}

// An empty list hides nothing, which is also the default for a new account.
func TestFilterHiddenModelsWithNoneHidden(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, hiddenModelsSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedHiddenTestUser(t, ds)

	in := []*models.Model{mdl("gpt-5.1"), mdl("gpt-4o")}
	require.Len(t, ds.filterHiddenModels(ctx, userID, in), 2)
}

// Losing preferences must show more, never less: an empty picker would look
// like the account had been broken.
func TestFilterHiddenModelsFailsOpen(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, hiddenModelsSchema)
	defer cleanup()
	in := []*models.Model{mdl("gpt-5.1"), mdl("gpt-4o")}
	// A user with no row at all stands in for an unreadable preference.
	require.Len(t, ds.filterHiddenModels(context.Background(), uuid.New(), in), 2)
}

// nil leaves the stored list alone; an empty non-nil slice clears it — the same
// contract favorites use, so the two behave alike.
func TestHiddenModelIDsNilVersusEmpty(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, hiddenModelsSchema)
	defer cleanup()
	ctx := context.Background()
	userID := seedHiddenTestUser(t, ds)

	_, err := ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{HiddenModelIDs: []string{"a", "b"}})
	require.NoError(t, err)

	// nil: unchanged.
	_, err = ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{Theme: "dark"})
	require.NoError(t, err)
	prefs, err := ds.GetUserPreferences(ctx, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "b"}, prefs.HiddenModelIDs)

	// empty non-nil: cleared.
	_, err = ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{HiddenModelIDs: []string{}})
	require.NoError(t, err)
	prefs, err = ds.GetUserPreferences(ctx, userID)
	require.NoError(t, err)
	require.Empty(t, prefs.HiddenModelIDs)
	require.NotNil(t, prefs.HiddenModelIDs, "must serialise as [] rather than null")
}
