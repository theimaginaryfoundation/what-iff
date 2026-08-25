package logging

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	EnvEnvironment = "ENV"
	EnvLogLevel    = "LOG_LEVEL"
)

func NewLogger() (*zap.Logger, error) {
	var config zap.Config
	environment := strings.ToLower(os.Getenv(EnvEnvironment))

	isDevelopment := environment == "dev" || environment == "development"

	if isDevelopment {
		config = zap.NewDevelopmentConfig()
	} else {
		config = zap.NewProductionConfig()
	}

	if logLevel := os.Getenv(EnvLogLevel); logLevel != "" {
		level, err := parseLogLevel(logLevel)
		if err != nil {
			return nil, err
		}
		config.Level = zap.NewAtomicLevelAt(level)
	}

	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return logger, nil
}

func parseLogLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("invalid LOG_LEVEL value: %q (expected 'debug', 'info', 'warn', or 'error')", level)
	}
}
