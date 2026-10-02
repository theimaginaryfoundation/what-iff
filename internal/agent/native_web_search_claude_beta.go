package agent

import (
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// webSearchToolCallsFromClaudeBetaMessages extracts native web search tool
// calls from Anthropic beta-MCP message payloads.
func webSearchToolCallsFromClaudeBetaMessages(msgs ...*anthropic.BetaMessage) []*models.ToolCall {
	var out []*models.ToolCall
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		for _, block := range msg.Content {
			ws, ok := block.AsAny().(anthropic.BetaWebSearchToolResultBlock)
			if !ok {
				continue
			}
			output := formatClaudeBetaWebSearchToolOutput(msg, ws)
			if output == "" {
				continue
			}
			out = append(out, &models.ToolCall{
				ToolName:   tools.ToolNameWebSearch,
				ToolInput:  claudeBetaWebSearchQueryForToolUseID(msg, ws.ToolUseID),
				ToolOutput: output,
			})
		}
	}
	return out
}

func formatClaudeBetaWebSearchToolOutput(msg *anthropic.BetaMessage, ws anthropic.BetaWebSearchToolResultBlock) string {
	var b strings.Builder
	if q := claudeBetaWebSearchQueryForToolUseID(msg, ws.ToolUseID); q != "" {
		fmt.Fprintf(&b, "Queries: %s\n", q)
	}
	cites := formatClaudeBetaWebSearchCitations(msg)
	body := provider.FormatClaudeBetaWebSearchResultBlock(ws)
	if cites != "" && provider.IsClaudeWebSearchEncryptedPlaceholder(body) {
		body = ""
	}
	if body != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(body)
	}
	if cites != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(cites)
	}
	return strings.TrimSpace(b.String())
}

func claudeBetaWebSearchQueryForToolUseID(msg *anthropic.BetaMessage, toolUseID string) string {
	if msg == nil {
		return ""
	}
	for _, block := range msg.Content {
		su, ok := block.AsAny().(anthropic.BetaServerToolUseBlock)
		if !ok {
			continue
		}
		if toolUseID != "" && su.ID != toolUseID {
			continue
		}
		if q := claudeWebSearchQueryFromServerToolInput(su.Input); q != "" {
			return q
		}
	}
	return ""
}

func formatClaudeBetaWebSearchCitations(msg *anthropic.BetaMessage) string {
	if msg == nil {
		return ""
	}
	return formatWebSearchCitationLines(collectClaudeBetaWebSearchCitations(msg))
}

func collectClaudeBetaWebSearchCitations(msg *anthropic.BetaMessage) []string {
	var lines []string
	seen := make(map[string]struct{})
	for _, block := range msg.Content {
		text, ok := block.AsAny().(anthropic.BetaTextBlock)
		if !ok {
			continue
		}
		for _, cite := range text.Citations {
			loc := cite.AsWebSearchResultLocation()
			if strings.TrimSpace(loc.URL) == "" && strings.TrimSpace(loc.Title) == "" {
				continue
			}
			line := formatWebSearchTitleURLLine(loc.Title, loc.URL)
			if line == "" {
				continue
			}
			key := line
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			lines = append(lines, line)
		}
	}
	return lines
}
