package provider

import (
	"context"
	"encoding/json"
)

// ToolUse is a normalized tool invocation from either provider.
// Claude's ToolUseBlock.Input is already json.RawMessage; the OpenAI path
// converts Arguments string → json.RawMessage via []byte(args).
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the outcome of executing a single ToolUse.
type ToolResult struct {
	ID     string
	Output string
	IsErr  bool
	// Images carries vision payloads produced by the tool (e.g. generate_image) for
	// injection into the next adapter Call. Anthropic requires images on user turns;
	// adapters append a follow-up user message after the tool_result block(s).
	Images []UserMessageImage
}

// toolResultOutput returns the string that should be sent back to the model for
// a ToolResult. It normalises the two empty-output edge cases so both adapters
// stay in sync if the default messages ever change.
func toolResultOutput(r ToolResult) string {
	if r.Output != "" {
		return r.Output
	}
	if r.IsErr {
		return "unknown error occurred"
	}
	return `{"message":"Tool executed successfully with no output"}`
}

func toolResultHasImageBytes(images []UserMessageImage) bool {
	for _, im := range images {
		if len(im.RawBytes) > 0 {
			return true
		}
	}
	return false
}

// AgentAdapter abstracts the SDK-specific mechanics of one tool-call round-trip.
// Each implementation is stateful — it holds the growing conversation internally
// so the caller drives the loop without ever touching SDK types directly.
//
// Provider-specific accessors (e.g. OpenAIAdapter.LastRawResponse for attachment
// metadata, OpenAIAdapter.CurrentParams for test inspection) are intentionally
// omitted from this interface: they have no meaningful equivalent across all
// providers and should be accessed on the concrete type at the call site that
// needs them.
type AgentAdapter interface {
	// Call makes one API request. The return values are mutually exclusive:
	// either a non-nil GenerateResponse (final text answer, empty ToolUse slice)
	// or a non-empty ToolUse slice (tool requests, nil GenerateResponse).
	// Returning both non-nil is an adapter bug and will be caught by handleAgentLoop.
	Call(ctx context.Context) (*GenerateResponse, []ToolUse, error)

	// AppendToolResults folds the executed tool results back into the adapter's
	// internal conversation state so the next Call has full context.
	AppendToolResults(results []ToolResult)

	// ForceFinalResponse strips the tool list, appends a nudge asking the model
	// to respond in prose, and issues one final Call. Used when maxToolCallRounds
	// is exhausted.
	ForceFinalResponse(ctx context.Context) (*GenerateResponse, error)

	// WebSearchCompletedCount returns how many completed native web searches were
	// observed across all Call/ForceFinalResponse rounds this turn (0 if unused).
	WebSearchCompletedCount() int

	// SetTextDeltaHandler installs an optional callback invoked with incremental
	// assistant text deltas when the provider supports streaming output.
	SetTextDeltaHandler(handler func(delta string))
}
