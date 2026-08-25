package agent

import "fmt"

type checkpointPolicy struct {
	MinAssistantMessagesSinceCheckpoint int
	MaxLastInputTokens                  int
	MaxEstimatedContextTokens           int
	// MinTurnsBetweenCheckpoints throttles the token-based triggers: even when a
	// token threshold is exceeded, no checkpoint fires until at least this many
	// assistant turns have elapsed since the last checkpoint. This stops tool-heavy
	// turns (e.g. job runs with large web-search results) from triggering compaction
	// on nearly every turn — with slab-based token accounting we'd rather carry a larger
	// context window than compact constantly. The scheduled turn-count trigger
	// (MinAssistantMessagesSinceCheckpoint) is unaffected and still forces a
	// checkpoint on schedule. Zero disables the throttle.
	MinTurnsBetweenCheckpoints int
}

type checkpointDecision struct {
	ShouldCheckpoint            bool
	Reason                      string
	AssistantMessagesSinceCheck int
}

type checkpointInputs struct {
	TotalAssistantMessages          int
	CheckpointAssistantMessageCount int
	LastInputTokens                 int
	EstimatedContextTokens          int
}

// decideCheckpoint evaluates triggers in priority order:
//  1. Scheduled turn-count trigger (MinAssistantMessagesSinceCheckpoint) — always fires
//     on schedule and is exempt from the throttle below.
//  2. Token-based triggers (MaxLastInputTokens, then MaxEstimatedContextTokens) — gated
//     by MinTurnsBetweenCheckpoints so tool-heavy turns can't compact every turn.
//
// The first matching trigger wins; preserve this ordering when adding new heuristics.
func decideCheckpoint(policy checkpointPolicy, in checkpointInputs) checkpointDecision {
	// Defensive normalization: treat negatives as zero.
	if in.TotalAssistantMessages < 0 {
		in.TotalAssistantMessages = 0
	}
	if in.CheckpointAssistantMessageCount < 0 {
		in.CheckpointAssistantMessageCount = 0
	}
	if in.LastInputTokens < 0 {
		in.LastInputTokens = 0
	}
	if in.EstimatedContextTokens < 0 {
		in.EstimatedContextTokens = 0
	}

	// since = assistant turns elapsed since the last checkpoint. CheckpointAssistantMessageCount
	// stores the total assistant-message count captured at the previous checkpoint, so the
	// difference is the number of new turns. It feeds both the scheduled trigger and the
	// min-turns throttle below.
	since := in.TotalAssistantMessages - in.CheckpointAssistantMessageCount
	if since < 0 {
		since = 0
	}

	decision := checkpointDecision{
		ShouldCheckpoint:            false,
		Reason:                      "",
		AssistantMessagesSinceCheck: since,
	}

	if policy.MinAssistantMessagesSinceCheckpoint > 0 && since >= policy.MinAssistantMessagesSinceCheckpoint {
		decision.ShouldCheckpoint = true
		decision.Reason = fmt.Sprintf("assistant_messages_since_checkpoint(%d) >= %d", since, policy.MinAssistantMessagesSinceCheckpoint)
		return decision
	}

	// Token-based triggers are throttled by MinTurnsBetweenCheckpoints so a single
	// tool-heavy turn cannot force compaction every turn. The scheduled turn-count
	// trigger above is intentionally exempt.
	if policy.MinTurnsBetweenCheckpoints > 0 && since < policy.MinTurnsBetweenCheckpoints {
		return decision
	}

	if policy.MaxLastInputTokens > 0 && in.LastInputTokens >= policy.MaxLastInputTokens {
		decision.ShouldCheckpoint = true
		decision.Reason = fmt.Sprintf("last_input_tokens(%d) >= %d", in.LastInputTokens, policy.MaxLastInputTokens)
		return decision
	}

	if policy.MaxEstimatedContextTokens > 0 && in.EstimatedContextTokens >= policy.MaxEstimatedContextTokens {
		decision.ShouldCheckpoint = true
		decision.Reason = fmt.Sprintf("estimated_context_tokens(%d) >= %d", in.EstimatedContextTokens, policy.MaxEstimatedContextTokens)
		return decision
	}

	return decision
}
