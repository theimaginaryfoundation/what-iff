package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		want    zapcore.Level
		wantErr bool
	}{
		{name: "debug", level: "debug", want: zapcore.DebugLevel},
		{name: "info", level: "info", want: zapcore.InfoLevel},
		{name: "warn", level: "warn", want: zapcore.WarnLevel},
		{name: "error", level: "error", want: zapcore.ErrorLevel},
		{name: "uppercase is normalized", level: "DEBUG", want: zapcore.DebugLevel},
		{name: "mixed case is normalized", level: "WaRn", want: zapcore.WarnLevel},
		{name: "unknown level", level: "trace", wantErr: true},
		{name: "empty string", level: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLogLevel(tt.level)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.level)
				assert.Equal(t, zapcore.InfoLevel, got, "default level is returned alongside the error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		logLevel  string
		wantLevel zapcore.Level
		wantErr   bool
	}{
		{
			name:      "production config by default",
			env:       "",
			wantLevel: zapcore.InfoLevel,
		},
		{
			name:      "development environment",
			env:       "development",
			wantLevel: zapcore.DebugLevel,
		},
		{
			name:      "dev shorthand, case-insensitive",
			env:       "DEV",
			wantLevel: zapcore.DebugLevel,
		},
		{
			name:      "explicit LOG_LEVEL overrides the environment default",
			env:       "production",
			logLevel:  "debug",
			wantLevel: zapcore.DebugLevel,
		},
		{
			name:     "invalid LOG_LEVEL fails the build",
			logLevel: "not-a-level",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvEnvironment, tt.env)
			t.Setenv(EnvLogLevel, tt.logLevel)

			logger, err := NewLogger()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, logger)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, logger)
			t.Cleanup(func() { _ = logger.Sync() })
			assert.True(t, logger.Core().Enabled(tt.wantLevel))
		})
	}
}
