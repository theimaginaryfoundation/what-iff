package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func newModelCatalogTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema)
}

func newModelCatalogRoleTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createUserRoleTestSchema)
}

func createCatalogTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("cat-" + id.String()[:8]).
		SetEmail("cat-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func createCatalogTestModel(t *testing.T, ds *Datastore, opts ...func(*ent.ModelCreate)) *ent.Model {
	t.Helper()
	create := ds.dbClient.Model.Create().
		SetName("model-" + uuid.NewString()[:8]).
		SetDisplayName("Test Model").
		SetDescription("test model")
	for _, opt := range opts {
		opt(create)
	}
	m, err := create.Save(context.Background())
	require.NoError(t, err)
	return m
}

func TestListModels(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	createCatalogTestModel(t, ds)
	deleted := createCatalogTestModel(t, ds)
	_, err := ds.dbClient.Model.UpdateOne(deleted).SetDeleted(true).Save(context.Background())
	require.NoError(t, err)

	got, err := ds.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1, "deleted models must be excluded")
}

func TestListModelsDefault(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	createCatalogTestModel(t, ds)

	got, err := ds.ListModelsDefault(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestListModelsForUser(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	createCatalogTestModel(t, ds)
	userID := createCatalogTestUser(t, ds)

	got, err := ds.ListModelsForUser(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestListModelsForUser_UserNotFound(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	createCatalogTestModel(t, ds)

	_, err := ds.ListModelsForUser(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestAdminPatchUserExperimentalModels_TogglesFlag(t *testing.T) {
	ds, cleanup := newModelCatalogRoleTestDatastore(t)
	defer cleanup()

	userID := createCatalogTestUser(t, ds)

	updated, err := ds.AdminPatchUserExperimentalModels(context.Background(), userID, true)
	require.NoError(t, err)
	require.True(t, updated.EnableExperimentalModels)

	updated, err = ds.AdminPatchUserExperimentalModels(context.Background(), userID, false)
	require.NoError(t, err)
	require.False(t, updated.EnableExperimentalModels)
}

func TestAdminPatchUserExperimentalModels_UserNotFound(t *testing.T) {
	ds, cleanup := newModelCatalogRoleTestDatastore(t)
	defer cleanup()

	_, err := ds.AdminPatchUserExperimentalModels(context.Background(), uuid.New(), true)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestAdminPatchUserExperimentalModels_RejectsSuperAdmin(t *testing.T) {
	ds, cleanup := newModelCatalogRoleTestDatastore(t)
	defer cleanup()

	userID := createCatalogTestUser(t, ds)
	roleID := createUserTestRole(t, ds, "super_admin")
	_, err := ds.dbClient.User.UpdateOneID(userID).AddRoleIDs(roleID).Save(context.Background())
	require.NoError(t, err)

	_, err = ds.AdminPatchUserExperimentalModels(context.Background(), userID, true)
	require.ErrorIs(t, err, ErrCannotModifySuperAdmin)
}

func TestAdminListModels(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	active := createCatalogTestModel(t, ds)
	deletedModel := createCatalogTestModel(t, ds)
	_, err := ds.dbClient.Model.UpdateOne(deletedModel).SetDeleted(true).Save(context.Background())
	require.NoError(t, err)

	got, err := ds.AdminListModels(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, active.ID, got[0].ID)

	got, err = ds.AdminListModels(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, deletedModel.ID, got[0].ID)
}

func TestAdminGetModel(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	m := createCatalogTestModel(t, ds)

	got, err := ds.AdminGetModel(context.Background(), m.ID)
	require.NoError(t, err)
	require.Equal(t, m.ID, got.ID)
}

func TestAdminGetModel_NotFound(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	_, err := ds.AdminGetModel(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestAdminUpdateModel(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	m := createCatalogTestModel(t, ds)
	newTier := "ultra"
	toolSupport := true
	credits := int64(5)

	got, err := ds.AdminUpdateModel(context.Background(), m.ID, models.UpdateModelRequest{
		Name:               "renamed-" + uuid.NewString()[:8],
		DisplayName:        "Renamed Model",
		Description:        "new description",
		Provider:           "anthropic",
		ToolSupport:        &toolSupport,
		BaseCreditsPerSlab: &credits,
		SubscriptionTier:   &newTier,
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed Model", got.DisplayName)
	require.Equal(t, "anthropic", got.Provider)
	require.True(t, got.ToolSupport)
	require.Equal(t, int64(5), got.BaseCreditsPerSlab)
	require.Equal(t, "ultra", got.SubscriptionTier)
}

func TestAdminUpdateModel_NotFound(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	_, err := ds.AdminUpdateModel(context.Background(), uuid.New(), models.UpdateModelRequest{})
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestAdminUpdateModel_NameConflict(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	existing := createCatalogTestModel(t, ds)
	target := createCatalogTestModel(t, ds)

	_, err := ds.AdminUpdateModel(context.Background(), target.ID, models.UpdateModelRequest{
		Name: existing.Name,
	})
	require.ErrorIs(t, err, ErrModelNameExists)
}

func TestAdminUpdateModel_InvalidProvider(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	m := createCatalogTestModel(t, ds)

	_, err := ds.AdminUpdateModel(context.Background(), m.ID, models.UpdateModelRequest{
		Provider: "not-a-real-provider",
	})
	require.ErrorIs(t, err, ErrInvalidProvider)
}

func TestAdminSetDefaultModel(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	first := createCatalogTestModel(t, ds, func(c *ent.ModelCreate) { c.SetIsDefault(true) })
	second := createCatalogTestModel(t, ds)

	got, err := ds.AdminSetDefaultModel(context.Background(), second.ID)
	require.NoError(t, err)
	require.True(t, got.IsDefault)

	reloadedFirst, err := ds.dbClient.Model.Get(context.Background(), first.ID)
	require.NoError(t, err)
	require.False(t, reloadedFirst.IsDefault, "old default must be cleared")
}

func TestAdminSetDefaultModel_NotFound(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	_, err := ds.AdminSetDefaultModel(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestResolveDefaultModelName(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	m := createCatalogTestModel(t, ds, func(c *ent.ModelCreate) { c.SetIsDefault(true) })

	got := ds.ResolveDefaultModelName(context.Background())
	require.Equal(t, m.Name, got)
}

func TestResolveDefaultModelName_FallsBackToEnvDefault(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	t.Setenv("DEFAULT_MODEL_NAME", "env-default-model")

	got := ds.ResolveDefaultModelName(context.Background())
	require.Equal(t, "env-default-model", got)
}

func TestGetModelByName(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	m := createCatalogTestModel(t, ds)

	got, err := ds.GetModelByName(context.Background(), m.Name)
	require.NoError(t, err)
	require.Equal(t, m.ID, got.ID)
}

func TestGetModelByName_EmptyName(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	_, err := ds.GetModelByName(context.Background(), "   ")
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestGetModelByName_NotFound(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	_, err := ds.GetModelByName(context.Background(), "no-such-model")
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestNormalizeModelProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    entmodel.Provider
		wantErr error
	}{
		{name: "empty defaults to openai", raw: "", want: entmodel.ProviderOpenai},
		{name: "openai", raw: " OpenAI ", want: entmodel.ProviderOpenai},
		{name: "anthropic", raw: "anthropic", want: entmodel.ProviderAnthropic},
		{name: "zai", raw: "zai", want: entmodel.ProviderZai},
		{name: "google", raw: "google", want: entmodel.ProviderGoogle},
		{name: "mistral", raw: "mistral", want: entmodel.ProviderMistral},
		{name: "deepseek", raw: "deepseek", want: entmodel.ProviderDeepseek},
		{name: "qwen", raw: "qwen", want: entmodel.ProviderQwen},
		{name: "xiaomi", raw: "xiaomi", want: entmodel.ProviderXiaomi},
		{name: "unknown", raw: "bogus", wantErr: ErrInvalidProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeModelProvider(tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeBaseCreditsPerSlab(t *testing.T) {
	t.Parallel()

	require.Equal(t, defaultBaseCreditsPerSlab, normalizeBaseCreditsPerSlab(0))
	require.Equal(t, defaultBaseCreditsPerSlab, normalizeBaseCreditsPerSlab(-5))
	require.Equal(t, int64(3), normalizeBaseCreditsPerSlab(3))
}

func TestResolveDefaultModelTx(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	m := createCatalogTestModel(t, ds, func(c *ent.ModelCreate) { c.SetIsDefault(true) })

	tx, err := ds.dbClient.Tx(context.Background())
	require.NoError(t, err)
	got, err := resolveDefaultModelTx(context.Background(), tx)
	require.NoError(t, err)
	require.Equal(t, m.ID, got.ID)
	require.NoError(t, tx.Commit())
}

func TestResolveDefaultModelTx_FallsBackToNamedDefault(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	t.Setenv("DEFAULT_MODEL_NAME", "named-default")
	named := createCatalogTestModel(t, ds, func(c *ent.ModelCreate) { c.SetName("named-default") })
	createCatalogTestModel(t, ds) // decoy, no is_default and different name

	tx, err := ds.dbClient.Tx(context.Background())
	require.NoError(t, err)
	got, err := resolveDefaultModelTx(context.Background(), tx)
	require.NoError(t, err)
	require.Equal(t, named.ID, got.ID)
	require.NoError(t, tx.Commit())
}

func TestResolveDefaultModelTx_FallsBackToAnyActiveModel(t *testing.T) {
	ds, cleanup := newModelCatalogTestDatastore(t)
	defer cleanup()

	t.Setenv("DEFAULT_MODEL_NAME", "no-such-model")
	only := createCatalogTestModel(t, ds)

	tx, err := ds.dbClient.Tx(context.Background())
	require.NoError(t, err)
	got, err := resolveDefaultModelTx(context.Background(), tx)
	require.NoError(t, err)
	require.Equal(t, only.ID, got.ID)
	require.NoError(t, tx.Commit())
}
