package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
)

// userCreatedTxHook, when non-nil, runs inside the user-creation transaction
// (both the standard and external-provider paths) right after the user and its
// preferences are inserted. A linked extension may set it to attach per-user
// setup that must be atomic with account creation; a returned error rolls the
// whole creation back. It is nil in the open-source build, so account creation
// has no extra transactional side effects.
var userCreatedTxHook func(ctx context.Context, tx *ent.Tx, userID uuid.UUID) error
