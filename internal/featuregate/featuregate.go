// Package featuregate is the boundary between the core and any entitlement
// policy that restricts optional features to some users. It exists so the core
// can ask "may this user use feature X?" without knowing how entitlement is
// decided (or that any such notion exists).
//
// In builds that do not link an implementation — the open-source distribution —
// no gate is registered and every feature is available to every user. A private
// build may register a Gate (e.g. one backed by the account's plan) to restrict
// features; linking that implementation is all that changes the behavior, with
// no edits to the call sites.
package featuregate

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
)

// Gate decides, per user, whether an optional feature may be used. Implementations
// must be safe for concurrent use.
type Gate interface {
	// IsEntitled reports whether userID may use the entitlement-gated features.
	// It should fail open only when that is the intended policy; the core treats a
	// false result as "not available for this account".
	IsEntitled(ctx context.Context, userID uuid.UUID) bool
}

// Active is the linked Gate. It is nil in builds that do not register one, in
// which case every feature is available (see the package doc). Server setup sets
// it from New when an implementation is linked.
var Active Gate

// New constructs the production Gate. It is nil in builds that do not link an
// implementation; the private implementation sets it in its init(). Server setup
// calls it (when non-nil) and assigns the result to Active.
var New func(ds *datastore.Datastore) Gate

// IsEntitled reports whether userID may use the entitlement-gated features. It
// returns true when no Gate is linked, so the open-source build gates nothing.
func IsEntitled(ctx context.Context, userID uuid.UUID) bool {
	if Active == nil {
		return true
	}
	return Active.IsEntitled(ctx, userID)
}
