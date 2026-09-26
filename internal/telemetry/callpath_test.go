package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallPathFromContext_default(t *testing.T) {
	t.Parallel()
	require.Equal(t, CallPathUnknown, CallPathFromContext(nil))
	require.Equal(t, CallPathUnknown, CallPathFromContext(context.Background()))
}

func TestWithCallPath_roundTrip(t *testing.T) {
	t.Parallel()
	ctx := WithCallPath(context.Background(), CallPathScratchpad)
	require.Equal(t, CallPathScratchpad, CallPathFromContext(ctx))
}

// Call path values are metric label values: renaming one splits dashboards, so pin the
// per-turn side-call paths.
func TestCallPath_sideCallValues(t *testing.T) {
	t.Parallel()
	require.Equal(t, CallPath("mode_select"), CallPathModeSelect)
	require.Equal(t, CallPath("expression_pick"), CallPathExpressionPick)
}
