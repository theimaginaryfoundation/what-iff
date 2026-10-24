package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestAdminSetUserExperimentalFeature(t *testing.T) {
	ds, cleanup := newModelCatalogRoleTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createCatalogTestUser(t, ds)

	has, err := ds.UserHasExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay)
	require.NoError(t, err)
	require.False(t, has, "off until an admin turns it on")

	updated, err := ds.AdminSetUserExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay, true)
	require.NoError(t, err)
	require.Equal(t, []string{models.ExperimentalFeatureBuzzRelay}, updated.ExperimentalFeatures)

	// Turning it on twice keeps one entry.
	updated, err = ds.AdminSetUserExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay, true)
	require.NoError(t, err)
	require.Equal(t, []string{models.ExperimentalFeatureBuzzRelay}, updated.ExperimentalFeatures)

	has, err = ds.UserHasExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay)
	require.NoError(t, err)
	require.True(t, has)

	updated, err = ds.AdminSetUserExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay, false)
	require.NoError(t, err)
	require.Empty(t, updated.ExperimentalFeatures)

	has, err = ds.UserHasExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay)
	require.NoError(t, err)
	require.False(t, has)
}

func TestAdminSetUserExperimentalFeature_Rejects(t *testing.T) {
	ds, cleanup := newModelCatalogRoleTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createCatalogTestUser(t, ds)

	_, err := ds.AdminSetUserExperimentalFeature(ctx, userID, "no_such_feature", true)
	require.ErrorIs(t, err, ErrUnknownExperimentalFeature)

	_, err = ds.AdminSetUserExperimentalFeature(ctx, uuid.New(), models.ExperimentalFeatureBuzzRelay, true)
	require.ErrorIs(t, err, ErrUserNotFound)

	roleID := createUserTestRole(t, ds, "super_admin")
	_, err = ds.dbClient.User.UpdateOneID(userID).AddRoleIDs(roleID).Save(ctx)
	require.NoError(t, err)
	_, err = ds.AdminSetUserExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay, true)
	require.ErrorIs(t, err, ErrCannotModifySuperAdmin)
}

func TestUserHasExperimentalFeature_SuperAdminAndUnknownUser(t *testing.T) {
	ds, cleanup := newModelCatalogRoleTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	has, err := ds.UserHasExperimentalFeature(ctx, uuid.New(), models.ExperimentalFeatureBuzzRelay)
	require.NoError(t, err)
	require.False(t, has, "an unknown user has nothing")

	userID := createCatalogTestUser(t, ds)
	roleID := createUserTestRole(t, ds, "super_admin")
	_, err = ds.dbClient.User.UpdateOneID(userID).AddRoleIDs(roleID).Save(ctx)
	require.NoError(t, err)
	has, err = ds.UserHasExperimentalFeature(ctx, userID, models.ExperimentalFeatureBuzzRelay)
	require.NoError(t, err)
	require.True(t, has, "super admins have every experimental feature")
}
