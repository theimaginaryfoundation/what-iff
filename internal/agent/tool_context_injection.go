package agent

import (
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const (
	// maxToolResultChars caps a single persisted tool result so one large payload
	// (e.g. a big find_context dump) can't dominate the carried-over context.
	maxToolResultChars = 4000
	// maxToolResultTotalChars bounds the total volume of persisted tool results
	// injected per build. Selection runs newest-first, so when the budget is
	// exhausted the oldest results are dropped first (see selectPersistedToolResults).
	maxToolResultTotalChars = 24000
	toolResultTruncatedNote = "\n… [tool result truncated]"
)

// selectPersistedToolResults resolves which prior-turn tool results should be carried
// into the current context. assistantTurns must be ordered oldest→newest; the newest
// turn is treated as one turn old (age 1) relative to the turn being built.
//
// Selection walks newest→oldest so that when the total character budget is exhausted
// the oldest results are evicted first. The returned slice is in chronological
// (oldest→newest) order — the same order the turns appear in history.
func selectPersistedToolResults(assistantTurns []*models.ChatMessage) []string {
	total := len(assistantTurns)
	if total == 0 {
		return nil
	}

	var selected []string
	budget := maxToolResultTotalChars

	for i := total - 1; i >= 0; i-- {
		msg := assistantTurns[i]
		if msg == nil || len(msg.ToolCalls) == 0 {
			continue
		}
		age := total - i // newest turn == 1
		for _, tc := range msg.ToolCalls {
			formatted, ok := formatPersistedToolResult(tc, age)
			if !ok {
				continue
			}
			if len(formatted) > budget {
				// Out of budget: stop entirely. Everything remaining is older, so this
				// realizes the "evict oldest first" rule.
				return reverseStrings(selected)
			}
			budget -= len(formatted)
			selected = append(selected, formatted)
		}
	}
	return reverseStrings(selected)
}

// formatPersistedToolResult applies the tool's context policy and renders the result
// text, or reports ok=false when the result should not be carried over.
func formatPersistedToolResult(tc *models.ToolCall, age int) (string, bool) {
	if tc == nil {
		return "", false
	}
	// Only successful results with actual output are worth re-injecting; errors and
	// empty acknowledgements add noise without recoverable context.
	output := strings.TrimSpace(tc.ToolOutput)
	if output == "" {
		return "", false
	}

	policy := toolContextPolicyFor(tc.ToolName)
	if !policy.persist {
		return "", false
	}
	if policy.ttlTurns > 0 && age > policy.ttlTurns {
		return "", false
	}

	if tc.ToolName == tools.ToolNameWebSearch {
		output = sanitizePersistedWebSearchOutput(output)
		if isPersistedWebSearchUseless(output) {
			return "", false
		}
	}

	if len(output) > maxToolResultChars {
		output = output[:maxToolResultChars] + toolResultTruncatedNote
	}

	turnLabel := "turn"
	if age != 1 {
		turnLabel = "turns"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Earlier tool result — %s (%d %s ago)]", tc.ToolName, age, turnLabel)
	if policy.staleWarning {
		fmt.Fprintf(&b, "\n⚠️ This is a snapshot from %d %s ago and may be stale; re-run the tool if you need current values.", age, turnLabel)
	}
	b.WriteString("\n")
	b.WriteString(output)
	return b.String(), true
}

// appendPersistedToolResults injects the selected tool results into modelCtx as a
// contiguous block of non-cacheable segments. It is appended after the history turns
// (i.e. after the cacheable prefix) so it never fragments the Claude cache prefix.
// Each result carries its own tool-name + age header, so grouping them here rather than
// inline preserves attribution without the per-message plumbing.
func appendPersistedToolResults(modelCtx *provider.ModelContext, results []string) {
	if modelCtx == nil || len(results) == 0 {
		return
	}
	modelCtx.Append(provider.SegmentKindToolResult, provider.RoleDeveloper,
		"Results from earlier tool calls in this conversation, carried forward so you don't have to re-run them:", false)
	for _, content := range results {
		modelCtx.Append(provider.SegmentKindToolResult, provider.RoleDeveloper, content, false)
	}
}

// sanitizePersistedWebSearchOutput strips Anthropic-native encrypted payloads and
// placeholder lines from persisted web search text so cross-provider turns never see
// opaque encrypted_content blobs or "encrypted by Anthropic" noise without URLs.
func sanitizePersistedWebSearchOutput(output string) string {
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			kept = append(kept, line)
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, `"encrypted_content"`) ||
			strings.Contains(lower, `"encrypted_index"`) ||
			strings.Contains(lower, `encrypted_content:`) {
			continue
		}
		if provider.IsClaudeWebSearchEncryptedPlaceholder(trimmed) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// isPersistedWebSearchUseless reports whether a sanitized web search snapshot has no
// portable value (query-only or citation-free encrypted stubs). Useful snapshots keep
// Results/Citations lines or bare URLs from the formatted persistence path.
func isPersistedWebSearchUseless(output string) bool {
	output = strings.TrimSpace(output)
	if output == "" {
		return true
	}
	if strings.Contains(output, "Citations:") ||
		strings.Contains(output, "Results:") ||
		strings.Contains(output, "http://") ||
		strings.Contains(output, "https://") {
		return false
	}
	return strings.HasPrefix(output, "Queries:") || provider.IsClaudeWebSearchEncryptedPlaceholder(output)
}

func reverseStrings(in []string) []string {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}
