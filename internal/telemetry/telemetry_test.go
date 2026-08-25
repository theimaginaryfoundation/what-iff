package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestInit_DisabledWhenNoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	telem, err := Init(context.Background(), zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, telem)
	require.NotNil(t, telem.Metrics)
	require.Nil(t, telem.meterProvider)
	require.Nil(t, telem.tracerProvider)
}
