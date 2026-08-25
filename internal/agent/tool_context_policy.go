package agent

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

// toolContextPolicy governs whether — and for how long — a tool call's result is
// re-injected into model context on subsequent turns. See the "Tool Result & File
// Context Persistence" design: tool outputs used to vanish after the turn that
// produced them, forcing redundant re-fetches and losing continuity. A policy is
// resolved per tool name at context-build time.
type toolContextPolicy struct {
	// persist controls whether the result is carried into later turns at all.
	persist bool
	// ttlTurns evicts the result after this many assistant turns have elapsed since
	// it was produced. Zero means no turn-based expiry (persist until compaction).
	ttlTurns int
	// staleWarning flags results whose freshness decays quickly (live-ish MCP data
	// like tickets/messages) so the model is told how old the snapshot is.
	staleWarning bool
}

// Default TTLs by tool category (in assistant turns). Named so tests and future
// tuning have a single source of truth.
const (
	webSearchTTLTurns   = 5
	mcpFileReadTTLTurns = 10
	mcpMessagesTTLTurns = 3
	noTTL               = 0
)

// MCP tool-name prefixes. Provider-side MCP tool calls are not currently persisted
// as models.ToolCall rows, but classifying them here keeps the policy layer complete
// and ready for when they are (and covers any locally-dispatched MCP shims).
const (
	mcpToolNamePrefix = "mcp__"
)

// toolContextPolicyFor resolves the persistence policy for a tool by name. Unknown
// tools default to persist-with-no-expiry: keeping context is the safer bias, and the
// per-result and total budgets in the context builder bound the cost. Live/metrics-style
// tools are the only category that opts out of persistence entirely.
func toolContextPolicyFor(toolName string) toolContextPolicy {
	name := strings.TrimSpace(toolName)

	switch name {
	case memoryEnrichmentToolCallName:
		// "Load Memory" is a display-only pseudo tool call recording the automatic
		// per-turn memory enrichment. That same context is re-fetched and injected fresh
		// every turn (SegmentKindMemoryContext), so persisting the historical dump would
		// only duplicate it. Never carry it over.
		return toolContextPolicy{persist: false}
	case tools.GenerateImageToolSpec.Name:
		// Generated images: keep for visual continuity, no expiry.
		return toolContextPolicy{persist: true, ttlTurns: noTTL}
	case tools.RunSubagentToolSpec.Name:
		// Subagent output is expensive to reproduce and often needed exactly when it
		// would otherwise have scrolled off — persist indefinitely.
		return toolContextPolicy{persist: true, ttlTurns: noTTL}
	case tools.ListToolSpec.Name,
		tools.ListMoodsToolSpec.Name:
		// Retrieval / listing results: stable enough to persist without expiry.
		return toolContextPolicy{persist: true, ttlTurns: noTTL}
	case tools.CreateMemoryToolSpec.Name,
		tools.UpdateScratchpadToolSpec.Name,
		tools.ChangeMoodToolSpec.Name,
		tools.CreateAgentJobToolSpec.Name:
		// Write/side-effect confirmations: small acknowledgements. Persisting them adds
		// little value and their effect is captured elsewhere (scratchpad, memories,
		// mood, jobs). Drop from cross-turn context.
		return toolContextPolicy{persist: false}
	case tools.ToolNameWebSearch:
		// Web search snapshots go stale; keep for a handful of turns.
		return toolContextPolicy{persist: true, ttlTurns: webSearchTTLTurns}
	}

	// MCP categories (prefix-matched). We can only coarsely classify by name substring
	// since MCP tool names are server-defined.
	if strings.HasPrefix(name, mcpToolNamePrefix) {
		return mcpToolContextPolicy(name)
	}

	return toolContextPolicy{persist: true, ttlTurns: noTTL}
}

// mcpToolContextPolicy classifies an MCP tool by coarse keyword matching on its name.
// tickets/messages → short TTL + stale warning; live metrics → no persistence;
// everything else (file/repo reads and general reads) → longer TTL.
func mcpToolContextPolicy(name string) toolContextPolicy {
	lower := strings.ToLower(name)
	switch {
	case containsAny(lower, "metric", "metrics", "live", "status", "health", "usage"):
		return toolContextPolicy{persist: false}
	case containsAny(lower, "ticket", "issue", "message", "comment", "thread", "jira", "slack"):
		return toolContextPolicy{persist: true, ttlTurns: mcpMessagesTTLTurns, staleWarning: true}
	default:
		return toolContextPolicy{persist: true, ttlTurns: mcpFileReadTTLTurns}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
