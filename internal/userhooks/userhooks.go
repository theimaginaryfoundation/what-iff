// Package userhooks is the boundary for optional side effects that run after a
// user account is created. It lets the core stay unaware of any account-lifecycle
// work a particular build wants to attach (e.g. provisioning an external record
// for the new user).
//
// In builds that do not link an implementation — the open-source distribution —
// no hook is registered and registration has no extra side effects. A private
// build may register one; server setup sets OnRegistered from New when an
// implementation is linked.
package userhooks

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Hook runs after a user successfully registers. It is best-effort: it owns its
// own error handling and must not block the registration response on slow work.
type Hook func(ctx context.Context, user *models.UserResponse)

// OnRegistered is the active post-registration hook, or nil when none is linked
// (the open-source build runs no post-registration side effects).
var OnRegistered Hook

// New constructs the production hook. It is nil in builds that do not link an
// implementation; the private build sets it in its init(). Server setup calls it
// (when non-nil) and assigns the result to OnRegistered.
var New func(ds *datastore.Datastore, logger *zap.Logger) Hook
