package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func newUtilityModelFixture(t *testing.T) (*Datastore, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	t.Cleanup(cleanup)
	alice, bob := uuid.New(), uuid.New()
	createTestUser(t, ds, alice)
	createTestUser(t, ds, bob)
	model := func(name string) uuid.UUID {
		m, err := ds.dbClient.Model.Create().SetName(name).SetDisplayName(name).SetDescription("").Save(context.Background())
		require.NoError(t, err)
		return m.ID
	}
	return ds, alice, bob, model("model-a"), model("model-b")
}

func TestUtilityModelSettings_ScopesAndReplacement(t *testing.T) {
	t.Parallel()
	ds, alice, bob, modelA, modelB := newUtilityModelFixture(t)
	ctx := context.Background()

	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleMemory, modelA))
	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleMemory, modelA))
	// A second choice replaces the first rather than adding a row.
	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleMemory, modelB))
	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, bob, models.UtilityRoleQuick, modelA))

	got, err := ds.ListUtilityModelSettings(ctx, alice)
	require.NoError(t, err)
	require.ElementsMatch(t, []models.UtilityModelSetting{
		{Scope: models.UtilityScopeDeployment, Role: models.UtilityRoleMemory, ModelID: modelA},
		{Scope: models.UtilityScopeAccount, Role: models.UtilityRoleMemory, ModelID: modelB},
	}, got, "the deployment's settings and alice's, never bob's")

	deployment, err := ds.ListUtilityModelSettings(ctx, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, deployment, 1)

	require.NoError(t, ds.ClearUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleMemory))
	got, err = ds.ListUtilityModelSettings(ctx, alice)
	require.NoError(t, err)
	require.Equal(t, []models.UtilityModelSetting{{Scope: models.UtilityScopeDeployment, Role: models.UtilityRoleMemory, ModelID: modelA}}, got)
}

// Deleting an account or a model deletes the settings that named it, so a role falls back
// rather than pointing at nothing.
func TestUtilityModelSettings_Cascade(t *testing.T) {
	t.Parallel()
	ds, alice, _, modelA, modelB := newUtilityModelFixture(t)
	ctx := context.Background()
	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleMemory, modelA))
	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleQuick, modelB))

	require.NoError(t, ds.dbClient.Model.DeleteOneID(modelB).Exec(ctx))
	deployment, err := ds.ListUtilityModelSettings(ctx, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, deployment)

	require.NoError(t, ds.dbClient.User.DeleteOneID(alice).Exec(ctx))
	n, err := ds.dbClient.UtilityModelSetting.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

// An account chooses from what it can use and the deployment, whose choice applies to every
// account, from the catalog; anything else is a model that doesn't exist for the chooser.
func TestSetUtilityModel_OnlyModelsTheChooserCanUse(t *testing.T) {
	t.Parallel()
	ds, alice, bob, modelA, _ := newUtilityModelFixture(t)
	ctx := context.Background()
	conn, err := ds.dbClient.ProviderConnection.Create().SetUserID(bob).SetPreset("ollama").SetName("Ollama").Save(ctx)
	require.NoError(t, err)
	bobs, err := ds.dbClient.Model.Create().SetName("bob-model").SetDisplayName("bob-model").SetDescription("").
		SetProvider("ollama").SetConnectionID(conn.ID).Save(ctx)
	require.NoError(t, err)
	off, err := ds.dbClient.Model.Create().SetName("retired").SetDisplayName("retired").SetDescription("").SetDeleted(true).Save(ctx)
	require.NoError(t, err)

	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, bob, models.UtilityRoleQuick, bobs.ID))
	require.ErrorIs(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleQuick, bobs.ID), ErrModelNotFound)
	require.ErrorIs(t, ds.SetUtilityModel(ctx, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleQuick, bobs.ID), ErrModelNotFound)
	require.ErrorIs(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleQuick, off.ID), ErrModelNotFound)
	require.ErrorIs(t, ds.SetUtilityModel(ctx, models.UtilityScopeAccount, alice, models.UtilityRoleQuick, uuid.New()), ErrModelNotFound)
	require.NoError(t, ds.SetUtilityModel(ctx, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleQuick, modelA))

	got, err := ds.ListUtilityModelSettings(ctx, alice)
	require.NoError(t, err)
	require.Equal(t, []models.UtilityModelSetting{{Scope: models.UtilityScopeDeployment, Role: models.UtilityRoleQuick, ModelID: modelA}}, got,
		"a refused choice leaves nothing behind")
}
