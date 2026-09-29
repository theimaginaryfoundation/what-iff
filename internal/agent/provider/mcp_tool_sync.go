package provider

import (
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// MCPToolNamePrefix marks dynamically loaded MCP function tools (mcp__<connector>__<tool>).
// Adapters use it to swap the MCP tool set between rounds without touching agent tools.
const MCPToolNamePrefix = "mcp__"

func isMCPToolName(name string) bool {
	return strings.HasPrefix(name, MCPToolNamePrefix)
}

// replaceChatCompletionMCPTools drops every mcp__ function tool from tools and appends mcpTools.
// A nil tool list (ForceFinalResponse already stripped tools) stays nil.
func replaceChatCompletionMCPTools(tools []openai.ChatCompletionToolUnionParam, mcpTools []openai.ChatCompletionToolUnionParam) []openai.ChatCompletionToolUnionParam {
	if tools == nil {
		return nil
	}
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools)+len(mcpTools))
	for _, t := range tools {
		if t.OfFunction != nil && isMCPToolName(t.OfFunction.Function.Name) {
			continue
		}
		out = append(out, t)
	}
	return append(out, mcpTools...)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *GeminiAdapter) SetMCPTools(mcpTools []openai.ChatCompletionToolUnionParam) {
	a.params.Tools = replaceChatCompletionMCPTools(a.params.Tools, mcpTools)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *MistralAdapter) SetMCPTools(mcpTools []openai.ChatCompletionToolUnionParam) {
	a.params.Tools = replaceChatCompletionMCPTools(a.params.Tools, mcpTools)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *DeepSeekAdapter) SetMCPTools(mcpTools []openai.ChatCompletionToolUnionParam) {
	a.params.Tools = replaceChatCompletionMCPTools(a.params.Tools, mcpTools)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *QwenAdapter) SetMCPTools(mcpTools []openai.ChatCompletionToolUnionParam) {
	a.params.Tools = replaceChatCompletionMCPTools(a.params.Tools, mcpTools)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *XiaomiAdapter) SetMCPTools(mcpTools []openai.ChatCompletionToolUnionParam) {
	a.params.Tools = replaceChatCompletionMCPTools(a.params.Tools, mcpTools)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *LocalAdapter) SetMCPTools(mcpTools []openai.ChatCompletionToolUnionParam) {
	a.params.Tools = replaceChatCompletionMCPTools(a.params.Tools, mcpTools)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
// Only the standard Messages path is updated; the legacy beta-MCP path carries no mcp__ tools.
func (a *ClaudeAdapter) SetMCPTools(mcpTools []anthropic.ToolUnionParam) {
	if a.params.Tools == nil {
		return
	}
	out := make([]anthropic.ToolUnionParam, 0, len(a.params.Tools)+len(mcpTools))
	for _, t := range a.params.Tools {
		if isMCPToolName(claudeToolName(t)) {
			continue
		}
		out = append(out, t)
	}
	a.params.Tools = append(out, mcpTools...)
}

// SetMCPTools replaces this turn's MCP tools so tools loaded mid-turn are callable next round.
func (a *OpenAIAdapter) SetMCPTools(mcpTools []responses.ToolUnionParam) {
	if a.params.Tools == nil {
		return
	}
	out := make([]responses.ToolUnionParam, 0, len(a.params.Tools)+len(mcpTools))
	for _, t := range a.params.Tools {
		if t.OfFunction != nil && isMCPToolName(t.OfFunction.Name) {
			continue
		}
		out = append(out, t)
	}
	a.params.Tools = append(out, mcpTools...)
}
