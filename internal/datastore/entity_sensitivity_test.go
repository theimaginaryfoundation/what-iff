package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Entities carry a sensitivity too; every read a chat makes filters on its limit in SQL.
func TestEntities_SensitivityGatesEveryRead(t *testing.T) {
	ds, cleanup := newEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)

	save := func(name string, level models.MemorySensitivity) *models.Entity {
		e, err := ds.SaveEntity(ctx, userID, nil, 0, models.EntityInput{Name: name, Card: name + " card", AuthorClass: "agent", Sensitivity: level})
		require.NoError(t, err)
		return e
	}
	pub := save("Pubby", models.MemorySensitivityPublic)
	per := save("Perry", "")
	sens := save("Senna", models.MemorySensitivitySensitive)
	require.Equal(t, models.MemorySensitivityPersonal, per.Sensitivity, "the default is personal")
	require.Equal(t, models.MemorySensitivitySensitive, sens.Sensitivity)

	names := func(es []*models.Entity) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Name)
		}
		return out
	}
	list := func(limit models.MemorySensitivity) []string {
		es, err := ds.ListEntities(ctx, userID, uuid.Nil, "", 10, limit)
		require.NoError(t, err)
		return names(es)
	}
	require.ElementsMatch(t, []string{"Pubby", "Perry", "Senna"}, list(""))
	require.ElementsMatch(t, []string{"Pubby", "Perry", "Senna"}, list(models.MemorySensitivitySensitive))
	require.ElementsMatch(t, []string{"Pubby", "Perry"}, list(models.MemorySensitivityPersonal))
	require.ElementsMatch(t, []string{"Pubby"}, list(models.MemorySensitivityPublic))

	// Mention spotting: the alias index only holds names the chat may see.
	aliases := func(limit models.MemorySensitivity) int {
		m, err := ds.ListEntityAliasesForScope(ctx, userID, uuid.Nil, 100, limit)
		require.NoError(t, err)
		return len(m)
	}
	require.Equal(t, 3, aliases(""))
	require.Equal(t, 2, aliases(models.MemorySensitivityPersonal))
	require.Equal(t, 1, aliases(models.MemorySensitivityPublic))

	// Lookup by name and by id: a hidden entity is simply not found.
	_, err := ds.FindEntityByName(ctx, userID, uuid.Nil, "senna", models.MemorySensitivityPersonal)
	require.ErrorIs(t, err, ErrEntityNotFound)
	got, err := ds.FindEntityByName(ctx, userID, uuid.Nil, "senna", "")
	require.NoError(t, err)
	require.Equal(t, sens.ID, got.ID)
	byIDs, err := ds.GetEntitiesByIDs(ctx, userID, []uuid.UUID{pub.ID, per.ID, sens.ID}, models.MemorySensitivityPersonal)
	require.NoError(t, err)
	require.Equal(t, []string{"Pubby", "Perry"}, names(byIDs))

	// An update that does not name a level keeps the stored one; naming one changes it.
	updated, err := ds.SaveEntity(ctx, userID, &sens.ID, 1, models.EntityInput{Name: "Senna", Card: "new card", AuthorClass: "user"})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivitySensitive, updated.Sensitivity)
	updated, err = ds.SaveEntity(ctx, userID, &sens.ID, 2, models.EntityInput{Name: "Senna", Card: "new card", AuthorClass: "user", Sensitivity: models.MemorySensitivityPublic})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivityPublic, updated.Sensitivity)
}
