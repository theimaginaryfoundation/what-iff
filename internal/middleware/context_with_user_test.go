package middleware

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
)

func TestContextWithUserSetsWhatTheAuthMiddlewaresSet(t *testing.T) {
	userID := uuid.New()
	ctx := ContextWithUser(context.Background(), userID, "Europe/Berlin")

	got, ok := GetUserIDFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, userID, got)
	fromAPI, ok := apicontext.UserIDFrom(ctx)
	assert.True(t, ok)
	assert.Equal(t, userID, fromAPI)
	assert.Equal(t, "Europe/Berlin", ctx.Value(ClientTimezoneKey))

	// It is enough for the agent's own context copy, which every turn goes through.
	copied, ok := CopyUserToIDContext(ctx, context.Background())
	assert.True(t, ok)
	got, _ = GetUserIDFromContext(copied)
	assert.Equal(t, userID, got)
}

func TestContextWithUserLeavesTimezoneUnsetWhenEmpty(t *testing.T) {
	ctx := ContextWithUser(context.Background(), uuid.New(), "")
	assert.Nil(t, ctx.Value(ClientTimezoneKey))
}
