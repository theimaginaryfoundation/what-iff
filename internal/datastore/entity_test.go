package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createEntityTestSchema adds the entity tables. Requires users (createMemoryImportTestSchema).
func createEntityTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE entities (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			type text NOT NULL DEFAULT '',
			card text NOT NULL DEFAULT '',
			revision integer NOT NULL DEFAULT 1,
			state text NOT NULL DEFAULT 'active',
			author_class text NOT NULL DEFAULT 'agent',
			pinned_personality_id uuid,
			card_updated_at datetime NOT NULL,
			user_entities uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE entity_alias (
			id uuid PRIMARY KEY,
			owner_id uuid NOT NULL,
			scope_key uuid NOT NULL,
			alias text NOT NULL,
			alias_norm text NOT NULL,
			entity_aliases uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX entityalias_owner_id_scope_key_alias_norm ON entity_alias (owner_id, scope_key, alias_norm)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newEntityTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createEntityTestSchema)
}

func TestSaveEntity_CreateUpdateConflictAndNames(t *testing.T) {
	ds, cleanup := newEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)

	john, err := ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{
		Name: "John Park", Type: "person", Card: "manager", Aliases: []string{"John", "my boss", "john park"},
		AuthorClass: models.WorkspaceAuthorAgent,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, john.Revision)
	assert.ElementsMatch(t, []string{"John", "my boss"}, john.Aliases, "the canonical name and duplicates aren't repeated as aliases")

	found, err := ds.FindEntityByName(ctx, userID, uuid.Nil, "MY BOSS", "")
	require.NoError(t, err)
	assert.Equal(t, john.ID, found.ID)

	_, err = ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: "Johnny", Card: "x", Aliases: []string{"John"}, AuthorClass: "agent"})
	var taken *EntityAliasTakenError
	require.True(t, errors.As(err, &taken), "got %v", err)
	assert.Equal(t, "John Park", taken.Owner)

	_, err = ds.SaveEntity(ctx, userID, &john.ID, 5, models.EntityInput{Name: "John Park", Card: "stale", AuthorClass: "agent"})
	var conflict *EntityConflictError
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, 1, conflict.Current)

	updated, err := ds.SaveEntity(ctx, userID, &john.ID, 1, models.EntityInput{Name: "John Park", Card: "on leave", Aliases: []string{"JP"}, AuthorClass: "user"})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Revision)
	assert.Equal(t, []string{"JP"}, updated.Aliases)
	assert.Equal(t, "user", updated.AuthorClass)
	_, err = ds.FindEntityByName(ctx, userID, uuid.Nil, "my boss", "")
	assert.ErrorIs(t, err, ErrEntityNotFound, "replaced aliases stop matching")

	// The freed name can be taken by another entity now.
	_, err = ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: "Boss Battle", Card: "x", Aliases: []string{"my boss"}, AuthorClass: "agent"})
	require.NoError(t, err)
}

func TestEntities_ScopesAndSpottingIndex(t *testing.T) {
	ds, cleanup := newEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	persona := uuid.New()

	shared, err := ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: "Vex", Card: "shared", AuthorClass: "agent"})
	require.NoError(t, err)
	pinned, err := ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: "Vex", Card: "campaign ranger", PinnedPersonalityID: &persona, AuthorClass: "agent"})
	require.NoError(t, err, "the same name may exist once per scope")

	got, err := ds.FindEntityByName(ctx, userID, persona, "vex", "")
	require.NoError(t, err)
	assert.Equal(t, pinned.ID, got.ID, "a personality's own entity wins over the shared one")
	got, err = ds.FindEntityByName(ctx, userID, uuid.New(), "vex", "")
	require.NoError(t, err)
	assert.Equal(t, shared.ID, got.ID, "other personalities see the shared one")

	matches, err := ds.ListEntityAliasesForScope(ctx, userID, persona, 100, "")
	require.NoError(t, err)
	assert.Len(t, matches, 2)
	pinnedSeen := false
	for _, m := range matches {
		assert.Equal(t, "vex", m.AliasNorm)
		if m.EntityID == pinned.ID {
			pinnedSeen = m.Pinned
		}
	}
	assert.True(t, pinnedSeen)

	list, err := ds.ListEntities(ctx, userID, uuid.Nil, "", 10, "")
	require.NoError(t, err)
	assert.Len(t, list, 1, "without a personality only shared entities are listed")

	byIDs, err := ds.GetEntitiesByIDs(ctx, userID, []uuid.UUID{pinned.ID, shared.ID}, "")
	require.NoError(t, err)
	require.Len(t, byIDs, 2)
	assert.Equal(t, pinned.ID, byIDs[0].ID, "results keep the requested order")

	stranger := createFATestUser(t, ds)
	none, err := ds.GetEntitiesByIDs(ctx, stranger, []uuid.UUID{pinned.ID}, "")
	require.NoError(t, err)
	assert.Empty(t, none)
	n, err := ds.CountActiveEntities(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestArchiveEntity_FreesNamesAndChecksRevision(t *testing.T) {
	ds, cleanup := newEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)

	e, err := ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: "Grog", Card: "barbarian", AuthorClass: "agent"})
	require.NoError(t, err)

	err = ds.ArchiveEntity(ctx, userID, e.ID, 3)
	var conflict *EntityConflictError
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, 1, conflict.Current)

	require.NoError(t, ds.ArchiveEntity(ctx, userID, e.ID, 1))
	_, err = ds.FindEntityByName(ctx, userID, uuid.Nil, "Grog", "")
	assert.ErrorIs(t, err, ErrEntityNotFound)
	matches, err := ds.ListEntityAliasesForScope(ctx, userID, uuid.Nil, 100, "")
	require.NoError(t, err)
	assert.Empty(t, matches)

	_, err = ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: "Grog", Card: "new grog", AuthorClass: "agent"})
	require.NoError(t, err, "a forgotten entity's name can be reused")
	assert.ErrorIs(t, ds.ArchiveEntity(ctx, userID, e.ID, 2), ErrEntityNotFound)
}
