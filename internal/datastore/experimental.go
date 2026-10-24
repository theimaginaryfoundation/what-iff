package datastore

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Per-user experimental features (models.ExperimentalFeatures). An admin turns one on for a user;
// a build that links a per-user experimental gate (featuregate.UserFlagExperimentalGate) then
// lets only those users, and super admins, use it.

// ErrUnknownExperimentalFeature is a feature name that is not in models.ExperimentalFeatures.
var ErrUnknownExperimentalFeature = errors.New("unknown experimental feature")

// AdminSetUserExperimentalFeature turns one experimental feature on or off for a user. Super
// admins have every feature and cannot be modified, as for experimental models.
func (d *Datastore) AdminSetUserExperimentalFeature(ctx context.Context, userID uuid.UUID, feature string, enabled bool) (*models.UserResponse, error) {
	if !models.IsExperimentalFeature(feature) {
		return nil, ErrUnknownExperimentalFeature
	}
	u, err := d.dbClient.User.Query().Where(user.ID(userID)).WithRoles().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	if isSuperAdmin(u) {
		return nil, ErrCannotModifySuperAdmin
	}
	features := slices.DeleteFunc(slices.Clone(u.ExperimentalFeatures), func(f string) bool { return f == feature })
	if enabled {
		features = append(features, feature)
	}
	if _, err := d.dbClient.User.UpdateOneID(userID).SetExperimentalFeatures(features).Save(ctx); err != nil {
		return nil, fmt.Errorf("update experimental features: %w", err)
	}
	u, err = d.dbClient.User.Query().Where(user.ID(userID)).WithPreferences().WithRoles().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("reload user: %w", err)
	}
	return toUserModel(u), nil
}

// UserHasExperimentalFeature reports whether a user may use an experimental feature under the
// per-user policy: an admin turned it on for them, or they are a super admin. An unknown user
// does not have it.
func (d *Datastore) UserHasExperimentalFeature(ctx context.Context, userID uuid.UUID, feature string) (bool, error) {
	u, err := d.dbClient.User.Query().Where(user.ID(userID)).WithRoles().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get user: %w", err)
	}
	return isSuperAdmin(u) || slices.Contains(u.ExperimentalFeatures, feature), nil
}

func isSuperAdmin(u *ent.User) bool {
	for _, role := range u.Edges.Roles {
		if role.Name == "super_admin" {
			return true
		}
	}
	return false
}
