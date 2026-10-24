package featuregate

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
)

// Experimental features (models.ExperimentalFeatures) are switched on for the server by their own
// operator switch (an environment variable the feature reads). This seam adds an optional
// per-user policy on top: the core asks ExperimentalEnabled, and a build that links an
// ExperimentalGate decides per user.
//
// In builds that link none (the open-source default) the operator switch decides for everyone.
// UserFlagExperimentalGate is the stock per-user policy (an admin turns a feature on for a user);
// a build links it, or its own, by setting NewExperimental in an init().

// ExperimentalGate decides, per user, whether an experimental feature is on for them.
// Implementations must be safe for concurrent use and should fail closed.
type ExperimentalGate interface {
	ExperimentalEnabled(ctx context.Context, userID uuid.UUID, feature string) bool
}

// ActiveExperimental is the linked ExperimentalGate, or nil. Server setup sets it from
// NewExperimental when a build links one.
var ActiveExperimental ExperimentalGate

// NewExperimental constructs the build's ExperimentalGate. It is nil unless a build sets it.
var NewExperimental func(ds *datastore.Datastore) ExperimentalGate

// ExperimentalEnabled reports whether feature is on for userID under the per-user policy. It is
// true when no gate is linked (the feature's operator switch alone decides). Callers check the
// operator switch themselves first.
func ExperimentalEnabled(ctx context.Context, userID uuid.UUID, feature string) bool {
	if ActiveExperimental == nil {
		return true
	}
	return ActiveExperimental.ExperimentalEnabled(ctx, userID, feature)
}

// UserFlagExperimentalGate turns an experimental feature on for the users an admin enabled it
// for (User.experimental_features) and for super admins. A lookup failure reads as off.
type UserFlagExperimentalGate struct {
	DS interface {
		UserHasExperimentalFeature(ctx context.Context, userID uuid.UUID, feature string) (bool, error)
	}
}

func (g UserFlagExperimentalGate) ExperimentalEnabled(ctx context.Context, userID uuid.UUID, feature string) bool {
	if g.DS == nil {
		return false
	}
	ok, err := g.DS.UserHasExperimentalFeature(ctx, userID, feature)
	return err == nil && ok
}
