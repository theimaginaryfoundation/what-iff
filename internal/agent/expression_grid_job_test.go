package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// EnqueueExpressionGridJob's cheapest branch is its own agent-not-configured
// guard, which fires before any datastore or background work.
func TestEnqueueExpressionGridJob_NilDatastoreReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	job, err := a.EnqueueExpressionGridJob(context.Background(), uuid.New(), uuid.New())
	require.Nil(t, job)
	require.Error(t, err)
	require.ErrorContains(t, err, "agent not configured")
}

func TestEnqueueExpressionCandidatesJob_NilDatastoreReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	job, err := a.EnqueueExpressionCandidatesJob(context.Background(), uuid.New(), uuid.New(), ExpressionGridKeys, nil)
	require.Nil(t, job)
	require.ErrorContains(t, err, "agent not configured")
}

func TestGenerateExpressionCandidates_NotConfigured(t *testing.T) {
	t.Parallel()

	_, err := (&Agent{}).GenerateExpressionCandidates(context.Background(), uuid.New(), uuid.New(), ExpressionGridKeys, nil)
	require.ErrorContains(t, err, "agent not configured")
}
