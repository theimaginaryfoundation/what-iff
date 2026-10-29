package metering

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AdvancedLimits bound one Advanced run (ADR 0x026) for an account. A zero field keeps the
// deployment default (the AGENT_ADVANCED_* environment); the agent still clamps every value to its
// hard upper bounds, so a limit here can lower a run's reach or raise it within those bounds, but
// never lift them.
type AdvancedLimits struct {
	MaxSteps      int
	MaxDuration   time.Duration
	CompactTokens int64
}

// AdvancedLimiter is an optional Meter capability: a deployment that sets Advanced limits per
// account or plan implements it on its Meter, and the agent asks it once at the start of each
// Advanced run. Meters without it (NoopMeter) leave the environment defaults.
type AdvancedLimiter interface {
	AdvancedLimits(ctx context.Context, userID uuid.UUID) AdvancedLimits
}
