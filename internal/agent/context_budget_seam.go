package agent

import "github.com/theimaginaryfoundation/what-iff/internal/models"

// contextBudget bundles the per-conversation "when to summarize / what context"
// knobs that govern a single chat turn:
//
//   - "when to summarize" — the token triggers and throttle fed into
//     decideCheckpoint's checkpointPolicy (see postprocessing_policy.go).
//   - "what context" — how much prior conversation is replayed each turn: the
//     history page size and the post-checkpoint carry-over bridge.
//
// Today every conversation runs on one fixed budget (defaultContextBudget). This
// struct is the seam that lets the private overlay vary the budget per
// conversation (e.g. capacity tiers) without touching any call site. The default
// path is a behavioral no-op: it resolves to the same constants the call sites
// used before the seam existed.
//
// Design rationale and the planned tier policy live in
// docs/adr/0x024-swappable-context-budget-policy.md.
type contextBudget struct {
	// "When to summarize" — fed into decideCheckpoint's checkpointPolicy.
	MaxLastInputTokens         int
	MaxEstimatedContextTokens  int
	MinTurnsBetweenCheckpoints int

	// "What context" — how much conversation is replayed each turn.
	HistoryPageSize    int
	CarryOverMaxTurns  int
	CarryOverMaxTokens int
}

// defaultContextBudget is the fixed, production-calibrated budget: the exact
// constants the call sites used before this seam existed. Resolving through it is
// a behavioral no-op, and it is the invariant the eventual "Standard" tier must
// reproduce byte-for-byte (see the ADR's migration note).
func defaultContextBudget() contextBudget {
	return contextBudget{
		MaxLastInputTokens:         checkpointMaxLastInputTokens,
		MaxEstimatedContextTokens:  checkpointMaxEstimatedContextTokens,
		MinTurnsBetweenCheckpoints: checkpointMinTurnsBetweenCheckpoints,
		HistoryPageSize:            defaultHistoryPageSize,
		CarryOverMaxTurns:          carryOverMaxTurns,
		CarryOverMaxTokens:         carryOverMaxTokens,
	}
}

// contextBudgetForChat, when non-nil, overrides the fixed default budget for a
// given chat. It is the swap-point for per-conversation context policy (capacity
// tiers, billing-gated budgets). The private overlay installs it from an init()
// in a file composed into this package, mirroring external_tools_seam.go. Nil =>
// the fixed default, which is exactly today's behavior.
//
// The resolver is intentionally a pure chat -> budget mapping: the active tier is
// read off the (overlay-owned) chat fields, so budget resolution stays off the
// billing hot path. Enforcing *which* tier a chat is allowed to hold is a
// separate concern handled where the tier is written, not here.
var contextBudgetForChat func(chat *models.Chat) contextBudget

// resolveContextBudget returns the active budget for a chat turn: the overlay's
// resolver if one is installed, otherwise the fixed default. A nil chat always
// resolves to the default.
func resolveContextBudget(chat *models.Chat) contextBudget {
	if contextBudgetForChat != nil && chat != nil {
		return contextBudgetForChat(chat)
	}
	return defaultContextBudget()
}
