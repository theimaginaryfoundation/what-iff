package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// HandleUserMessageSync's cheapest branch is the missing-user-in-context
// guard, which fires before any datastore call.
func TestHandleUserMessageSync_NoUserInContextReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	msg, err := a.HandleUserMessageSync(context.Background(), models.ChatMessage{})
	require.Nil(t, msg)
	require.Error(t, err)
	require.ErrorContains(t, err, "user ID not found in context")
}
