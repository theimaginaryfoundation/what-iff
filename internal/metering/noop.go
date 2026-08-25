package metering

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// NoopMeter is the Meter used when no metering implementation is linked (e.g. the
// open-source distribution). Every turn is allowed and served without recording
// usage, and Record only emits a debug log — nothing is tracked. This is what lets the
// private implementation be removed wholesale without touching the agent or
// server: the wiring simply falls back here.
type NoopMeter struct {
	// Logger is optional; when nil, Record logs nothing.
	Logger *zap.Logger
}

// Check always allows the turn without recording usage.
func (m NoopMeter) Check(_ context.Context, _ uuid.UUID, _, _ string) Decision {
	return Decision{Allowed: true, DontRecordUsage: true}
}

// Record logs the metered turn at debug level and does nothing else.
func (m NoopMeter) Record(_ context.Context, _ Decision, u Usage) {
	if m.Logger == nil {
		return
	}
	m.Logger.Debug("metering(noop): turn not tracked",
		zap.String("user_id", u.UserID.String()),
		zap.String("action_type", u.ActionType),
		zap.String("model", u.Model),
		zap.Int64("tokens", u.Tokens),
	)
}
