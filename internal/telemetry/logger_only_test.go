package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLoggerOnly_nilUsesNop(t *testing.T) {
	t.Parallel()
	tel := LoggerOnly(nil)
	require.NotNil(t, tel)
	require.NotNil(t, tel.Logger)
	require.Nil(t, tel.Metrics)
}

func TestLoggerOnly_preservesLogger(t *testing.T) {
	t.Parallel()
	log := zap.NewExample()
	tel := LoggerOnly(log)
	require.Equal(t, log, tel.Logger)
}
