package apicontext

import (
	"context"

	"github.com/google/uuid"
)

// userIDKey is the request-scoped key for the authenticated API user (same
// logical value as middleware.UserIDKey, but defined here so packages such as
// datastore can read the actor without importing middleware and creating an
// import cycle).
type userIDKey struct{}

// WithUserID returns a derived context that carries the authenticated user id.
func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

// UserIDFrom returns the authenticated user id when present.
func UserIDFrom(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return v, ok
}
