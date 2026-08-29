package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// EnqueuePersonalityPortraitJob's cheapest branch is its own
// agent-not-configured guard, which fires before the system-prompt check or
// any datastore call.
func TestEnqueuePersonalityPortraitJob_NilDatastoreReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	job, err := a.EnqueuePersonalityPortraitJob(context.Background(), uuid.New(), uuid.New(), "a system prompt", "auto")
	require.Nil(t, job)
	require.Error(t, err)
	require.ErrorContains(t, err, "agent not configured")
}
