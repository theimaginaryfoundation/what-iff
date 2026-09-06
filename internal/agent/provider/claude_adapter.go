package provider

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

var claudeUnsupportedSchemaKeys = map[string]struct{}{
	"minimum":  {},
	"maximum":  {},
	"maxItems": {},
	"default":  {},
}

// claudeToolName extracts the name from a Claude ToolUnionParam (function tools only).
func claudeToolName(t anthropic.ToolUnionParam) string {
	if t.OfTool != nil {
		return t.OfTool.Name
	}
	return ""
}

// claudeWebSearchTool adds Anthropic's native web search capability.
var claudeWebSearchTool = anthropic.ToolUnionParam{
	OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{},
}

// ClaudeFunctionTool builds an Anthropic function tool param from a provider-neutral
// function spec. Tool selection stays in the agent layer; this only handles SDK shape.
func ClaudeFunctionTool(name, description string, properties map[string]interface{}, required []string, strict bool) anthropic.ToolUnionParam {
	sanitizedProperties := sanitizeClaudeSchemaProperties(properties)
	if required == nil {
		required = []string{}
	}

	return anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        name,
			Description: anthropic.String(description),
			Strict:      anthropic.Bool(strict),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: sanitizedProperties,
				Required:   required,
				ExtraFields: map[string]any{
					"additionalProperties": false,
				},
			},
		},
	}
}

func sanitizeClaudeSchemaProperties(properties map[string]interface{}) map[string]interface{} {
	sanitized, _ := sanitizeClaudeSchemaValue(properties).(map[string]interface{})
	return sanitized
}

func sanitizeClaudeSchemaValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, inner := range val {
			if _, unsupported := claudeUnsupportedSchemaKeys[k]; unsupported {
				continue
			}
			out[k] = sanitizeClaudeSchemaValue(inner)
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(val))
		for k, inner := range val {
			out[k] = inner
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, inner := range val {
			out = append(out, sanitizeClaudeSchemaValue(inner))
		}
		return out
	case []string:
		out := make([]interface{}, 0, len(val))
		for _, inner := range val {
			out = append(out, inner)
		}
		return out
	default:
		return val
	}
}

// ClaudeAdapter implements AgentAdapter for the Anthropic Messages API.
// It is stateful: the params.Messages slice grows as tool-result turns are
// appended between rounds, mirroring the full conversation for each API call.
type ClaudeAdapter struct {
	provider *ClaudeProvider
	params   anthropic.MessageNewParams

	// webSearchCompleted counts assistant-side web_search_tool_result blocks seen this turn.
	webSearchCompleted int
	rawMessages        []*anthropic.Message
	textDeltaHandler   func(delta string)
}

// NewClaudeAdapter constructs a ClaudeAdapter from pre-built MessageNewParams.
// It appends the standard agent tool list and, when toolsEnabled, the native
// web-search tool.
// disabledTools, if non-nil, removes tools whose names appear in the set.
// The caller is responsible for system, model, and message history in params.
func NewClaudeAdapter(provider *ClaudeProvider, params anthropic.MessageNewParams, functionTools []anthropic.ToolUnionParam, webSearchEnabled bool, disabledTools map[string]bool) *ClaudeAdapter {
	for _, t := range functionTools {
		if disabledTools[claudeToolName(t)] {
			continue
		}
		params.Tools = append(params.Tools, t)
	}
	if webSearchEnabled {
		params.Tools = append(params.Tools, claudeWebSearchTool)
	}
	return &ClaudeAdapter{
		provider: provider,
		params:   params,
	}
}

// Call makes one Messages API request. It returns a final GenerateResponse when
// the model produces a text answer, or a non-empty []ToolUse when tools are
// requested (GenerateResponse is nil in that case).
func (a *ClaudeAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	var (
		msg *anthropic.Message
		err error
	)
	if a.textDeltaHandler != nil {
		msg, err = a.provider.CallWithRetryStreaming(ctx, a.params, a.textDeltaHandler)
	} else {
		msg, err = a.provider.Call(ctx, a.params)
	}
	if err != nil {
		return nil, nil, WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Anthropic API call failed: %w", err))
	}
	a.webSearchCompleted += countWebSearchToolResultsInMessage(msg)
	a.rawMessages = append(a.rawMessages, msg)

	toolUses := extractClaudeToolUses(msg)
	if len(toolUses) == 0 {
		return a.provider.ToGenerateResponse(msg), nil, nil
	}

	// Persist assistant loop context for the next Call: replay native web search blocks
	// (with encrypted_content) via per-block ToParam(), not full msg.ToParam().
	appendClaudeAssistantLoopTurn(&a.params, msg, toolUses)

	return nil, toolUses, nil
}

// AppendToolResults converts results to Anthropic tool-result content blocks and
// appends them as a single user turn so the next Call has full context. Tool-produced
// images are appended in a following user turn (Anthropic forbids image blocks on
// assistant turns — same split as HistoryAssistantImageCaption for history).
func (a *ClaudeAdapter) AppendToolResults(results []ToolResult) {
	var blocks []anthropic.ContentBlockParamUnion
	for _, r := range results {
		blocks = append(blocks, anthropic.NewToolResultBlock(r.ID, toolResultOutput(r), r.IsErr))
	}
	a.params.Messages = append(a.params.Messages, anthropic.NewUserMessage(blocks...))
	appendClaudeToolResultImages(&a.params, results)
}

func (a *ClaudeAdapter) SetTextDeltaHandler(handler func(delta string)) {
	a.textDeltaHandler = handler
}

// ForceFinalResponse strips tools, appends a nudge, and issues one last Call.
func (a *ClaudeAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	a.params.Tools = nil
	a.params.Messages = append(a.params.Messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock("Please provide your best final response based on the information gathered so far without additional tool calls."),
	))

	var (
		msg *anthropic.Message
		err error
	)
	if a.textDeltaHandler != nil {
		msg, err = a.provider.CallWithRetryStreaming(ctx, a.params, a.textDeltaHandler)
	} else {
		msg, err = a.provider.Call(ctx, a.params)
	}
	if err != nil {
		return nil, WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Anthropic final-response call failed: %w", err))
	}
	a.webSearchCompleted += countWebSearchToolResultsInMessage(msg)
	a.rawMessages = append(a.rawMessages, msg)
	return a.provider.ToGenerateResponse(msg), nil
}

// AllRawMessages returns Anthropic message payloads observed this turn.
func (a *ClaudeAdapter) AllRawMessages() []*anthropic.Message {
	if len(a.rawMessages) == 0 {
		return nil
	}
	out := make([]*anthropic.Message, len(a.rawMessages))
	copy(out, a.rawMessages)
	return out
}

// WebSearchCompletedCount returns how many web search result blocks were observed
// across all adapter Call and ForceFinalResponse invocations this turn.
func (a *ClaudeAdapter) WebSearchCompletedCount() int {
	return a.webSearchCompleted
}

func countWebSearchToolResultsInMessage(msg *anthropic.Message) int {
	if msg == nil {
		return 0
	}
	n := 0
	for _, block := range msg.Content {
		if _, ok := block.AsAny().(anthropic.WebSearchToolResultBlock); ok {
			n++
		}
	}
	return n
}

// extractClaudeToolUses returns all tool-use blocks from a Message normalised
// to the provider-agnostic ToolUse type.
func extractClaudeToolUses(msg *anthropic.Message) []ToolUse {
	if msg == nil {
		return nil
	}
	var uses []ToolUse
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			uses = append(uses, ToolUse{
				ID:    tb.ID,
				Name:  tb.Name,
				Input: tb.Input,
			})
		}
	}
	return uses
}

// appendClaudeAssistantLoopTurn appends the assistant turn for the in-turn agent loop.
// Client tool_use, server_tool_use, and web_search_tool_result blocks are replayed via
// per-block ToParam() (preserving encrypted_content for Anthropic server-side decryption).
// Full Message.ToParam() is avoided — it can mangle web search content on resubmit.
// Non-replayable web search payloads fall back to formatted user text.
func appendClaudeAssistantLoopTurn(params *anthropic.MessageNewParams, msg *anthropic.Message, uses []ToolUse) {
	if params == nil || msg == nil || len(uses) == 0 {
		return
	}
	useIDs := make(map[string]struct{}, len(uses))
	for _, u := range uses {
		useIDs[u.ID] = struct{}{}
	}

	var blocks []anthropic.ContentBlockParamUnion
	var webSearchTextFallback []string

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			if strings.TrimSpace(v.Text) != "" {
				// Citations on intermediate native web-search text are owned by
				// Anthropic. Some responses contain an empty location URL, which
				// their API rejects when this turn is replayed. The text itself is
				// useful, while the encrypted web-search result below preserves
				// native search continuity, so deliberately omit citations here.
				blocks = append(blocks, anthropic.NewTextBlock(v.Text))
			}
		case anthropic.ServerToolUseBlock:
			blocks = append(blocks, block.ToParam())
		case anthropic.WebSearchToolResultBlock:
			if claudeWebSearchToolResultReplayable(v) {
				blocks = append(blocks, block.ToParam())
			} else if out := FormatClaudeWebSearchResultBlock(v); out != "" {
				webSearchTextFallback = append(webSearchTextFallback, out)
			}
		case anthropic.ToolUseBlock:
			if _, ok := useIDs[v.ID]; ok {
				blocks = append(blocks, block.ToParam())
			}
		}
	}

	if len(blocks) == 0 {
		return
	}
	params.Messages = append(params.Messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleAssistant,
		Content: blocks,
	})
	if len(webSearchTextFallback) > 0 {
		appendClaudeInLoopWebSearchContextText(params, ClaudeInLoopWebSearchContextHeader+strings.Join(webSearchTextFallback, "\n\n"))
	}
}

func claudeWebSearchToolResultReplayable(ws anthropic.WebSearchToolResultBlock) bool {
	if len(claudeWebSearchResultsFromContent(ws.Content)) > 0 {
		return true
	}
	err := ws.Content.AsResponseWebSearchToolResultError()
	return err.ErrorCode != ""
}

func appendClaudeToolResultImages(params *anthropic.MessageNewParams, results []ToolResult) {
	if params == nil {
		return
	}
	for _, r := range results {
		blocks := claudeImageBlocksFromUserImages(r.Images, GeneratedToolImageCaption)
		if len(blocks) == 0 {
			continue
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(blocks...))
	}
}

func appendClaudeInLoopWebSearchContextText(params *anthropic.MessageNewParams, text string) {
	if params == nil || strings.TrimSpace(text) == "" {
		return
	}
	params.Messages = append(params.Messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock(text),
	))
}
