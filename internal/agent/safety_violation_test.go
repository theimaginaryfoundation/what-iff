package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// handleSafetyViolation's cheapest branch is its own nil-violation guard,
// which fires before chatMessage/chatCtx are dereferenced.
func TestHandleSafetyViolation_NilViolationReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	msg, resp, err := a.handleSafetyViolation(context.Background(), uuid.New(), nil, nil, nil)
	require.Nil(t, msg)
	require.Nil(t, resp)
	require.Error(t, err)
	require.ErrorContains(t, err, "safety violation details missing")
}
