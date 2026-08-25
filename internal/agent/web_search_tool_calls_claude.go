package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// webSearchToolCallsFromClaudeMessages extracts native web search result blocks from
// Anthropic message payloads observed during a turn.
func webSearchToolCallsFromClaudeMessages(msgs ...*anthropic.Message) []*models.ToolCall {
	var out []*models.ToolCall
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		for _, block := range msg.Content {
			ws, ok := block.AsAny().(anthropic.WebSearchToolResultBlock)
			if !ok {
				continue
			}
			output := formatClaudeWebSearchToolOutput(msg, ws)
			if output == "" {
				continue
			}
			out = append(out, &models.ToolCall{
				ToolName:   tools.ToolNameWebSearch,
				ToolInput:  claudeWebSearchQueryForToolUseID(msg, ws.ToolUseID),
				ToolOutput: output,
			})
		}
	}
	return out
}

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

func formatClaudeWebSearchToolOutput(msg *anthropic.Message, ws anthropic.WebSearchToolResultBlock) string {
	var b strings.Builder
	if q := claudeWebSearchQueryForToolUseID(msg, ws.ToolUseID); q != "" {
		fmt.Fprintf(&b, "Queries: %s\n", q)
	}
	cites := formatClaudeWebSearchCitations(msg)
	body := provider.FormatClaudeWebSearchResultBlock(ws)
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

func claudeWebSearchQueryForToolUseID(msg *anthropic.Message, toolUseID string) string {
	if msg == nil {
		return ""
	}
	for _, block := range msg.Content {
		su, ok := block.AsAny().(anthropic.ServerToolUseBlock)
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

func claudeWebSearchQueryFromServerToolInput(input any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	var payload struct {
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if len(payload.Queries) > 0 {
		return strings.Join(payload.Queries, "; ")
	}
	return strings.TrimSpace(payload.Query)
}

func formatClaudeWebSearchCitations(msg *anthropic.Message) string {
	if msg == nil {
		return ""
	}
	return formatWebSearchCitationLines(collectClaudeWebSearchCitations(msg))
}

func formatClaudeBetaWebSearchCitations(msg *anthropic.BetaMessage) string {
	if msg == nil {
		return ""
	}
	return formatWebSearchCitationLines(collectClaudeBetaWebSearchCitations(msg))
}

func collectClaudeWebSearchCitations(msg *anthropic.Message) []string {
	var lines []string
	seen := make(map[string]struct{})
	for _, block := range msg.Content {
		text, ok := block.AsAny().(anthropic.TextBlock)
		if !ok {
			continue
		}
		for _, cite := range text.Citations {
			loc := cite.AsWebSearchResultLocation()
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

func formatWebSearchCitationLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "Citations:\n" + strings.Join(lines, "\n")
}
