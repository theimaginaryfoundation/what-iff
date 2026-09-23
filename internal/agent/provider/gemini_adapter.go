package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// GeminiAdapter implements AgentAdapter for Google's OpenAI-compatible Chat
// Completions API. It is stateful: the params.Messages slice grows as assistant
// tool-call turns and tool-result turns are appended between rounds, mirroring the
// full conversation for each API call (same pattern as ClaudeAdapter).
//
// Not safe for concurrent use: a single adapter instance is owned by one agent-loop
// goroutine for the duration of a chat turn.
//
// When a text-delta handler is set the turn streams token deltas; otherwise it
// falls back to a single non-streaming request. The full assistant text is always
// available via GenerateResponse.
type GeminiAdapter struct {
	provider         *GeminiProvider
	params           openai.ChatCompletionNewParams
	logger           *zap.Logger
	callSeq          int
	lastRequested    []ToolUse
	textDeltaHandler func(delta string)
	// thought signatures recovered from the most recent streamed response, keyed
	// by tool-call index; re-attached to the assistant tool-call replay turn
	// because Gemini rejects a follow-up request that omits them. Empty/nil for
	// non-streaming calls, where the response's own raw JSON already carries them.
	// No locking: the adapter is owned by one agent-loop goroutine for the turn
	// (see the type doc), so this field is written and read by that goroutine only.
	lastThoughtSignatures map[int64]string
}

// NewGeminiAdapter constructs a GeminiAdapter from pre-built params. functionTools
// are appended (minus any whose name appears in disabledTools). The caller is
// responsible for messages and model in params. logger may be nil (logging is skipped).
func NewGeminiAdapter(provider *GeminiProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool, logger *zap.Logger) *GeminiAdapter {
	for _, t := range functionTools {
		if t.OfFunction != nil && disabledTools[t.OfFunction.Function.Name] {
			continue
		}
		params.Tools = append(params.Tools, t)
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GeminiAdapter{
		provider: provider,
		params:   params,
		logger:   logger,
	}
}

// Call makes one Chat Completions request. It returns a final GenerateResponse when
// the model produces a text answer, or a non-empty []ToolUse when tools are requested
// (GenerateResponse is nil in that case).
func (a *GeminiAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	a.callSeq++
	seq := a.callSeq
	a.logger.Debug("gemini chat completion request",
		zap.Int("call_seq", seq),
		zap.Int("message_count", len(a.params.Messages)),
		zap.Int("tool_count", len(a.params.Tools)),
	)

	resp, err := a.call(ctx)
	if err != nil {
		a.logger.Error("gemini chat completion failed",
			zap.Int("call_seq", seq),
			zap.String("api_error_detail", truncateLogString(openAIErrorDetail(err), 2048)),
			zap.Error(err),
		)
		a.logger.Debug("gemini chat completion message tail",
			zap.Int("call_seq", seq),
			zap.String("message_tail", summarizeGeminiMessages(a.params.Messages)),
		)
		return nil, nil, WrapProviderCallError(models.SafetyViolationProviderGoogle, "Gemini API call failed", err)
	}
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("Gemini API returned no choices")
	}

	toolUses := normalizeGeminiToolUses(extractChatCompletionToolUses(resp))
	if len(toolUses) == 0 {
		a.lastRequested = nil
		return a.toGenerateResponse(resp), nil, nil
	}

	a.lastRequested = toolUses
	names := make([]string, len(toolUses))
	ids := make([]string, len(toolUses))
	for i, u := range toolUses {
		names[i] = u.Name
		ids[i] = u.ID
	}
	hasThoughtSig := make([]bool, 0, len(resp.Choices[0].Message.ToolCalls))
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		hasThoughtSig = append(hasThoughtSig, geminiToolCallHasThoughtSignature(tc))
	}
	a.logger.Debug("gemini requested tool calls",
		zap.Int("call_seq", seq),
		zap.Strings("tool_names", names),
		zap.Strings("tool_call_ids", ids),
		zap.Bools("has_thought_signature", hasThoughtSig),
	)

	// Persist a Gemini-compat assistant turn so AppendToolResults can match tool_call_id.
	a.params.Messages = append(a.params.Messages, geminiAssistantToolCallMessage(resp.Choices[0].Message, a.lastThoughtSignatures))
	return nil, toolUses, nil
}

// AppendToolResults appends one tool-role message per result so the next Call has
// full context.
func (a *GeminiAdapter) AppendToolResults(results []ToolResult) {
	for i, r := range results {
		toolName := a.toolNameForResult(r.ID, i)
		a.logger.Debug("gemini appending tool result",
			zap.Int("call_seq", a.callSeq),
			zap.String("tool_name", toolName),
			zap.String("tool_call_id", r.ID),
			zap.Bool("is_error", r.IsErr),
			zap.Int("content_len", len(toolResultOutput(r))),
		)
		a.params.Messages = append(a.params.Messages, geminiToolResultMessage(r, toolName))
	}
}

func (a *GeminiAdapter) toolNameForResult(toolCallID string, index int) string {
	for _, u := range a.lastRequested {
		if u.ID == toolCallID {
			return u.Name
		}
	}
	if index >= 0 && index < len(a.lastRequested) {
		return a.lastRequested[index].Name
	}
	return ""
}

// ForceFinalResponse strips tools, appends a nudge, and issues one last Call.
func (a *GeminiAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	a.params.Tools = nil
	a.params.Messages = append(a.params.Messages, openai.UserMessage(
		"Please provide your best final response based on the information gathered so far without additional tool calls.",
	))
	resp, err := a.call(ctx)
	if err != nil {
		a.logger.Error("gemini final-response call failed",
			zap.String("api_error_detail", truncateLogString(openAIErrorDetail(err), 2048)),
			zap.Error(err),
		)
		return nil, WrapProviderCallError(models.SafetyViolationProviderGoogle, "Gemini final-response call failed", err)
	}
	return a.toGenerateResponse(resp), nil
}

// toGenerateResponse converts a final response, stripping a leading echo of the
// internal tool-call placeholder when the outbound messages showed it to the model
// as assistant output (see geminiToolCallContentPlaceholder). The streamed path is
// filtered in call; this covers non-streamed turns and a streamed turn whose
// deltas were entirely echo (runGeneration then falls back to this Text).
func (a *GeminiAdapter) toGenerateResponse(resp *openai.ChatCompletion) *GenerateResponse {
	out := a.provider.ToGenerateResponse(resp)
	if geminiMessagesCarryToolCallPlaceholder(a.params.Messages) {
		out.Text = stripGeminiToolCallEcho(out.Text)
	}
	return out
}

// call streams when a text-delta handler is set, else issues a non-streaming request.
// The streaming path also captures per-tool-call thought signatures (dropped by the
// accumulator) for the tool-call replay turn; the non-streaming response carries them
// in its own raw JSON, so lastThoughtSignatures is cleared there.
func (a *GeminiAdapter) call(ctx context.Context) (*openai.ChatCompletion, error) {
	if a.textDeltaHandler != nil {
		onDelta := a.textDeltaHandler
		var echo *geminiToolCallEchoFilter
		if geminiMessagesCarryToolCallPlaceholder(a.params.Messages) {
			// The model has seen the internal placeholder as its own prior output
			// and may imitate it; keep any echo out of the user-visible stream.
			echo = newGeminiToolCallEchoFilter(onDelta)
			onDelta = echo.HandleDelta
		}
		resp, thoughtSignatures, err := a.provider.CallStreaming(ctx, a.params, onDelta)
		if echo != nil {
			echo.Flush()
		}
		if err != nil {
			// Do not retain signatures from a failed call.
			a.lastThoughtSignatures = nil
			return nil, err
		}
		a.lastThoughtSignatures = thoughtSignatures
		return resp, nil
	}
	a.lastThoughtSignatures = nil
	return a.provider.Call(ctx, a.params)
}

// WebSearchCompletedCount is always 0 — the Gemini path has no native web search tool.
func (a *GeminiAdapter) WebSearchCompletedCount() int { return 0 }

// SetTextDeltaHandler stores the handler; when set, the Gemini path streams token deltas.
func (a *GeminiAdapter) SetTextDeltaHandler(handler func(delta string)) {
	a.textDeltaHandler = handler
}

// extractChatCompletionToolUses pulls function tool calls from the first choice and
// normalises them to the provider-agnostic ToolUse type.
func extractChatCompletionToolUses(resp *openai.ChatCompletion) []ToolUse {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	var uses []ToolUse
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if tc.Type != "" && tc.Type != "function" {
			continue
		}
		uses = append(uses, ToolUse{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: []byte(tc.Function.Arguments),
		})
	}
	return uses
}

// extractGeminiToolUses is an alias for extractChatCompletionToolUses kept for Gemini call sites.
func extractGeminiToolUses(resp *openai.ChatCompletion) []ToolUse {
	return extractChatCompletionToolUses(resp)
}

// openAIErrorDetail extracts the JSON error body from openai-go API errors when present.
func openAIErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr != nil {
		if raw := apiErr.RawJSON(); raw != "" {
			return raw
		}
		if dump := apiErr.DumpResponse(true); len(dump) > 0 {
			return strings.TrimSpace(string(dump))
		}
		return apiErr.Error()
	}
	return err.Error()
}

func truncateLogString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…(truncated)"
}

// summarizeGeminiMessages returns a compact JSON snapshot of the tail of the outbound
// messages payload to speed up debugging compat-layer 400s.
func summarizeGeminiMessages(messages []openai.ChatCompletionMessageParamUnion) string {
	const tail = 4
	start := 0
	if len(messages) > tail {
		start = len(messages) - tail
	}
	type msgSummary struct {
		Role       string `json:"role,omitempty"`
		ToolCallID string `json:"tool_call_id,omitempty"`
		Name       string `json:"name,omitempty"`
		HasTools   bool   `json:"has_tool_calls,omitempty"`
		ContentLen int    `json:"content_len,omitempty"`
	}
	out := make([]msgSummary, 0, len(messages)-start)
	for _, m := range messages[start:] {
		s := msgSummary{}
		if role := m.GetRole(); role != nil {
			s.Role = *role
		}
		if id := m.GetToolCallID(); id != nil {
			s.ToolCallID = *id
		}
		if name := m.GetName(); name != nil {
			s.Name = *name
		}
		if calls := m.GetToolCalls(); len(calls) > 0 {
			s.HasTools = true
		}
		switch {
		case m.OfTool != nil && !param.IsOmitted(m.OfTool.Content.OfString):
			s.ContentLen = len(m.OfTool.Content.OfString.Value)
		case m.OfAssistant != nil && !param.IsOmitted(m.OfAssistant.Content.OfString):
			s.ContentLen = len(m.OfAssistant.Content.OfString.Value)
		case m.OfUser != nil && !param.IsOmitted(m.OfUser.Content.OfString):
			s.ContentLen = len(m.OfUser.Content.OfString.Value)
		}
		out = append(out, s)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}
