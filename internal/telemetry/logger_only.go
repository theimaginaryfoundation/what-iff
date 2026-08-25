package telemetry

import "go.uber.org/zap"

// LoggerOnly returns a Telemetry with only Logger set (Metrics and Tracer nil).
// Use for unit tests and narrow wiring when full Init() is not used.
func LoggerOnly(logger *zap.Logger) *Telemetry {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Telemetry{Logger: logger}
}
