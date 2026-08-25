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
