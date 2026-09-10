package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecallDistiller_Distill_Unavailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var nilDistiller *recallDistiller
	_, err := nilDistiller.Distill(ctx, "q", "m")
	require.ErrorContains(t, err, "OpenAIProvider unavailable")

	d := newRecallDistiller(nil)
	_, err = d.Distill(ctx, "q", "m")
	require.ErrorContains(t, err, "OpenAIProvider unavailable")

	d = newRecallDistiller(&Agent{})
	_, err = d.Distill(ctx, "q", "m")
	require.ErrorContains(t, err, "OpenAIProvider unavailable")
}
