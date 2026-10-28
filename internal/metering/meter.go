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
// Check runs before a turn's inference to decide whether it may proceed, returning a Decision.
// That Decision (the turn's gate Decision) is handed back to every Record the turn makes so the
// implementation can settle it with the context it captured at gate time. A Standard turn records
// once, when it completes. An Advanced turn (ADR 0x026) records once per finished step and once
// at the end, all with the gate Decision, even an hour later: the implementation must read
// anything that can change during the run (balances, caps) at Record time, not from the snapshot.
// The Checks an Advanced run makes before its further calls return Decisions that are only read
// for Allowed and Err; they are never passed to Record. The agent treats a Decision as opaque
// apart from Allowed, DontRecordUsage and Err.
type Meter interface {
	// Check reports whether userID may start a turn on a model of the given
	// tier ("low"/"medium"/"high"/"ultra"; "" means unknown, treated
	// conservatively), for the given action type (see models.ActionType*).
	//
	// The check is intentionally fuzzy — concurrent requests are not serialised
	// here; the implementation's atomic accounting at Record time is the precise
	// enforcer. A returned Decision with Allowed=false must hard-reject the turn.
	//
	// A running Advanced turn also calls Check before each model call after its first, with
	// the usage it has not recorded yet on ctx (WithPendingUsage). Allowed=false then stops the
	// run before that call. A Check that could not decide (a database error) sets Err as well as
	// Allowed=false, so the run can tell that from a refusal and does not end as out of quota.
	Check(ctx context.Context, userID uuid.UUID, tier, actionType string) Decision

	// Record settles the cost of one completed turn, or of one finished step of an Advanced turn
	// (fire-and-forget; it must not block normal flow and swallows its own errors). d is the gate
	// Decision of the turn; u describes what actually happened. The agent calls it with a context
	// that is not cancelled when the turn is stopped, so a write on it can complete.
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
	// Err is set, with Allowed=false, when the implementation could not decide (an
	// infrastructure failure) rather than refusing on quota. A turn's gate treats both alike; a
	// running Advanced turn ends as failed rather than out of quota.
	Err error
}

// Usage describes one completed turn for Meter.Record. It carries only facts the
// agent legitimately owns — never any metering internals; the implementation
// derives all of those from ActionType, Tokens, and the Decision's State.
type Usage struct {
	UserID     uuid.UUID
	ActionType string // models.ActionType* — already resolved (e.g. image generation)
	Model      string
	ChatID     string
	// Tokens is the input of the turn's final model call (on every path, Standard and
	// Advanced): the basis of slab pricing for implementations that price a turn on one call.
	// When CallsReported is set, Calls is the complete account of what this record covers and
	// Tokens must not be priced on top of it.
	Tokens int64
	// MessageID is the assistant message this turn produced — the same message
	// that owns the turn's Context X-ray snapshot. Optional (empty when no
	// assistant message exists, e.g. a failed turn); it carries no metering
	// meaning here. An implementation may persist it so per-turn cost can later
	// be looked up by message; NoopMeter ignores it.
	MessageID string
	// Metadata augments the recorded usage event (e.g. cancellation details).
	Metadata map[string]interface{}
	// SubagentRun marks the usage as originating from a subagent job.
	SubagentRun bool
	// Source is where the turn was started from (TurnSourceApp, TurnSourceWebhook, or a plugin's
	// own name such as "discord"); "" when the entry point did not say. See TurnSource.
	Source string
	// WebSearchCount is the number of billable web actions this turn (ADR 0x021): the
	// provider's native searches, or, when WebSearchFirstParty is set, successful
	// first-party web_search and fetch_page calls.
	// Only meaningful when ActionType is models.ActionTypeWebSearch.
	WebSearchCount int
	// WebSearchFirstParty marks WebSearchCount as first-party tool calls rather than
	// vendor-native searches, which cost the operator very different amounts, so an
	// implementation can price them separately.
	WebSearchFirstParty bool
	// ImageQuality is the quality tier ("low"|"medium"|"high") of one image the
	// generate_image tool produced, so an implementation can price it. Only
	// meaningful when ActionType is models.ActionTypeImageGeneration; empty means the
	// turn itself is an image-generation ritual, which the implementation prices as a
	// fixed low-quality image.
	ImageQuality string
	// ImageProvider is the vendor (a models.ModelProvider) the generate_image tool
	// made the image with, so an implementation can price vendors separately. Only meaningful when ActionType is
	// models.ActionTypeImageGeneration; empty means OpenAI, the only vendor before
	// users could choose one.
	ImageProvider string
	// ReasoningEffort is the reasoning effort ("off"|"low"|"medium"|"high") the turn was run
	// at when the user explicitly selected one, the model honours it, and it is above the
	// model's native level (models.ReasoningEffortAboveNative; ADR 0x026). Empty when no effort
	// was selected, and when the selection asks for no more than the model reasons at unasked
	// (low on a model that defaults to medium), so an implementation that prices effort leaves
	// those turns as they were and never charges more for a lower effort.
	ReasoningEffort string
	// AgentLoop is the loop the turn ran ("standard"|"advanced"; ADR 0x026). Empty means
	// standard.
	AgentLoop string
	// Calls are the model calls this record covers, in order, with each call's token usage
	// (ADR 0x026). A Standard turn's record carries every call of the turn (tool rounds and the
	// reply). An Advanced turn is recorded step by step as it runs (Step > 0), each record carrying
	// that step's calls, and the turn's final record carries whatever calls remain.
	Calls []CallUsage
	// CallsReported says Calls is complete for this record, including when it is empty (an
	// Advanced turn's final record after every call was in a step record: nothing left to
	// charge). False means per-call usage is unknown, and only then is Tokens the whole basis.
	CallsReported bool
	// Step is the Advanced step a step record covers (1-based); 0 for a turn's final record and
	// for Standard turns.
	Step int
	// TurnID identifies the turn (its job) across its step records and final record, so an
	// implementation can total a turn's charges. Empty when the turn has no job.
	TurnID string
}

// CallUsage is one model call's token usage as the provider reported it. InputTokens includes
// CachedInputTokens and CacheWriteTokens; OutputTokens includes ReasoningTokens where the provider
// reports them separately (Anthropic reports thinking only as output).
type CallUsage struct {
	// Model is the model the call ran on; it can differ from Usage.Model (a sub-agent's model).
	Model             string
	InputTokens       int64
	CachedInputTokens int64
	// CacheWriteTokens are input tokens written to the provider's prompt cache on this call
	// (Anthropic's cache_creation_input_tokens), which some providers bill above the input rate.
	CacheWriteTokens int64
	OutputTokens     int64
	ReasoningTokens  int64
}

// New constructs the production Meter. It is nil in builds that do not link a
// metering implementation; callers must fall back to NoopMeter in that case (see
// the NoopMeter doc). The private implementation sets this in its init().
//
// The implementation owns all of its own configuration, reading whatever
// environment it needs at construction time. The core passes only the datastore
// and logger, so no implementation settings cross this boundary.
var New func(ds *datastore.Datastore, logger *zap.Logger) Meter
