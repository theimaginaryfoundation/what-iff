package agent

import (
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Vendor-native web search (ADR 0x021): recovers the provider's built-in web search calls
// from raw responses so they are persisted like any other tool call. This is the fallback
// when first-party web search is not configured; runGeneration only wires it up when
// Agent.FirstPartyWebSearch is false.

// mergeWebSearchToolCalls appends native web search records without duplicating identical output.
func mergeWebSearchToolCalls(existing []*models.ToolCall, native []*models.ToolCall) []*models.ToolCall {
	if len(native) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(native))
	for _, tc := range existing {
		if tc == nil {
			continue
		}
		seen[webSearchDedupeKey(tc)] = struct{}{}
	}
	out := append([]*models.ToolCall(nil), existing...)
	for _, tc := range native {
		if tc == nil {
			continue
		}
		key := webSearchDedupeKey(tc)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tc)
	}
	return out
}

func webSearchDedupeKey(tc *models.ToolCall) string {
	if tc == nil {
		return ""
	}
	return tc.ToolName + "\x00" + tc.ToolInput + "\x00" + tc.ToolOutput
}
