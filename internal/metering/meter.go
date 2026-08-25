// Package metering defines the boundary between the core agent and any
// usage-metering implementation. The agent depends only on the Meter interface
// and the plain-data Decision/Usage types declared here — never on a concrete
// implementation or its datastore.
//
// Two implementations satisfy Meter:
//
//   - NoopMeter (this package): allows every turn and records nothing. It is the
//     default for builds that do not link a metering implementation (e.g. the
//     open-source distribution).
//   - an external implementation, linked in privately: it registers its
//     constructor into the New factory var below via an init(), so linking that
//     package (a blank import in main) is all that swaps the production meter in.
//     Removing the package — and its blank import — leaves the core compiling
//     against NoopMeter with no edits to the agent or server wiring.
package metering

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// Meter gates and records metered turns. Implementations must be safe for
// concurrent use; both Check and Record are called from per-request goroutines.
//
// The two calls form a pair around a single turn: Check runs before inference to
// decide whether the turn may proceed, returning a Decision. That same Decision
// is handed back to Record after the turn completes so the implementation can
// settle it with the context it captured at gate time. The agent treats the
// Decision as opaque apart from its Allowed flag.
type Meter interface {
	// Check reports whether userID may start a turn on a model of the given
	// tier ("low"/"medium"/"high"/"ultra"; "" means unknown, treated
	// conservatively), for the given action type (see models.ActionType*).
	//
	// The check is intentionally fuzzy — concurrent requests are not serialised
	// here; the implementation's atomic accounting at Record time is the precise
	// enforcer. A returned Decision with Allowed=false must hard-reject the turn.
	Check(ctx context.Context, userID uuid.UUID, tier, actionType string) Decision

	// Record settles the cost of one completed turn (fire-and-forget; it must not
	// block normal flow and swallows its own errors). d is the Decision returned by
	// the Check that gated this turn; u describes what actually happened.
	Record(ctx context.Context, d Decision, u Usage)
}

// Decision is the result of Meter.Check. Allowed and DontRecordUsage are the only
// fields the agent reads; State is opaque implementation data (a snapshot taken at
// gate time) that the agent round-trips back into Record unchanged.
type Decision struct {
	// Allowed is false when the turn must be hard-rejected.
	Allowed bool
	// DontRecordUsage is true when the turn is served without being counted against
	// the user's usage. The agent may surface this as a UI hint; it carries no
	// metering logic of its own.
	DontRecordUsage bool
	// State is private to the Meter implementation that produced this Decision.
	// The agent must not inspect or construct it — only pass it back into Record.
	State any
}

// Usage describes one completed turn for Meter.Record. It carries only facts the
// agent legitimately owns — never any metering internals; the implementation
// derives all of those from ActionType, Tokens, and the Decision's State.
type Usage struct {
	UserID     uuid.UUID
	ActionType string // models.ActionType* — already resolved (e.g. image generation)
	Model      string
	ChatID     string
	Tokens     int64
	// Metadata augments the recorded usage event (e.g. cancellation details).
	Metadata map[string]interface{}
	// SubagentRun marks the usage as originating from a subagent job.
	SubagentRun bool
	// WebSearchCount is the number of native web searches performed this turn.
	// Only meaningful when ActionType is models.ActionTypeWebSearch.
	WebSearchCount int
}

// New constructs the production Meter. It is nil in builds that do not link a
// metering implementation; callers must fall back to NoopMeter in that case (see
// the NoopMeter doc). The private implementation sets this in its init().
//
// The implementation owns all of its own configuration, reading whatever
// environment it needs at construction time. The core passes only the datastore
// and logger, so no implementation settings cross this boundary.
var New func(ds *datastore.Datastore, logger *zap.Logger) Meter
