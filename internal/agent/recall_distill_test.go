package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRecallDistiller_Distill_Unavailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var nilDistiller *recallDistiller
	_, err := nilDistiller.Distill(ctx, "q", "m")
	require.ErrorContains(t, err, "no agent")

	d := newRecallDistiller(nil)
	_, err = d.Distill(ctx, "q", "m")
	require.ErrorContains(t, err, "no agent")

	// Without an OpenAI client, the memory role's model is still asked: here, none is chosen.
	d = newRecallDistiller(&Agent{logger: zap.NewNop()})
	_, err = d.Distill(ctx, "q", "m")
	require.ErrorIs(t, err, ErrUtilityRoleNotSet)
}
