package datastore

import (
	"context"
	"database/sql"
	"testing"

	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createPersonalityCardTestSchema adds the personality_cards table. It must run after
// createMemoryImportTestSchema, which creates the personalities table it references.
func createPersonalityCardTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE personality_cards (
		id uuid PRIMARY KEY,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		data json NOT NULL,
		omitted_fields json,
		personality_card uuid NOT NULL UNIQUE REFERENCES personalities(id) ON DELETE CASCADE
	)`)
	require.NoError(t, err)
}

func newPersonalityCardTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createPersonalityCardTestSchema)
}

func TestCreatePersonalityStoresCharacterCardAndExportReadsItBack(t *testing.T) {
	ds, cleanup := newPersonalityCardTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))

	card := map[string]any{"first_mes": "Hello!", "tags": []any{"fox"}, "extensions": map[string]any{"x": "y"}}
	created, err := ds.CreatePersonality(ctx, userID, models.Personality{
		Name: "Ada", SystemPrompt: "You are Ada.", ImageStyle: "auto",
		CharacterCard: &models.PersonalityCard{Data: card, OmittedFields: []string{"scenario"}},
	})
	require.NoError(t, err)
	require.Nil(t, created.CharacterCard, "reads never carry the blob")

	got, err := ds.GetPersonalityCard(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, card, got.Data)
	require.Equal(t, []string{"scenario"}, got.OmittedFields)

	inputs, err := ds.ExportPersonalityInputs(ctx, userID)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.Equal(t, card, inputs[0].CharacterCard)
	require.Equal(t, []string{"scenario"}, inputs[0].OmittedCardFields)
}

func TestGetPersonalityCardIsNilForNativePersonalities(t *testing.T) {
	ds, cleanup := newPersonalityCardTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))

	created, err := ds.CreatePersonality(ctx, userID, models.Personality{Name: "Native", SystemPrompt: "sp", ImageStyle: "auto"})
	require.NoError(t, err)

	got, err := ds.GetPersonalityCard(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Nil(t, got)

	inputs, err := ds.ExportPersonalityInputs(ctx, userID)
	require.NoError(t, err)
	require.Nil(t, inputs[0].CharacterCard)
}

func TestGetPersonalityCardIsOwnerScoped(t *testing.T) {
	ds, cleanup := newPersonalityCardTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	ownerID, otherID := uuid.New(), uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, ownerID))
	require.NoError(t, insertMemoryMergeTestUser(t, ds, otherID))

	created, err := ds.CreatePersonality(ctx, ownerID, models.Personality{
		Name: "Ada", SystemPrompt: "sp", ImageStyle: "auto", CharacterCard: &models.PersonalityCard{Data: map[string]any{"secret": "mine"}},
	})
	require.NoError(t, err)

	_, err = ds.GetPersonalityCard(ctx, otherID, created.ID)
	require.ErrorIs(t, err, ErrPersonalityNotFound)
	_, err = ds.GetPersonalityCard(ctx, ownerID, uuid.New())
	require.ErrorIs(t, err, ErrPersonalityNotFound)
}

// The sqlite tables above are hand-written, so pin the property that matters in the real
// (generated) schema: deleting a personality must take its card with it.
func TestPersonalityCardCascadesWithItsPersonality(t *testing.T) {
	require.Equal(t, entschema.Cascade, migrate.PersonalityCardsTable.ForeignKeys[0].OnDelete)
}
