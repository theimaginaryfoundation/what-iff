package agent

import (
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

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
