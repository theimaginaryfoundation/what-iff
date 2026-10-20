package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// reasoningChatAdapter is the AgentAdapter core shared by the OpenAI-compatible Chat Completions
// providers that return reasoning as a reasoning_content field next to content (DeepSeek, Qwen).
// It captures that reasoning (streamed and non-streamed), streams it live, and echoes each call's
// reasoning back on the assistant turn it belongs to: DeepSeek rejects a tool-call follow-up
// without it (400), and Qwen reads it when preserve_thinking is on. A turn with thinking off simply
// has no reasoning to capture or echo.
//
// Lifecycle of the per-call state:
//   - call records each response's reasoning into reasoning (the whole turn's, for
//     GenerateResponse.Reasoning) and lastReasoning (this call's only).
//   - A call that requests tools appends its assistant turn at once, with lastReasoning, through
//     replay.toolTurn; the next call overwrites lastReasoning.
//   - A call that answers is not appended (the Standard loop ends there): it is kept in final.last.
//     If the Advanced loop continues, ContinueWith appends it with lastReasoning (replay.finalTurn),
//     clears final.last, then appends the continuation as a user turn.
type reasoningChatAdapter struct {
	// name labels errors ("DeepSeek API call failed").
	name   string
	safety models.SafetyViolationProvider
	send   reasoningChatSender
	replay assistantReasoningReplay

	params           openai.ChatCompletionNewParams
	tools            scopedToolList[openai.ChatCompletionToolUnionParam]
	textDeltaHandler func(delta string)

	// reasoning collects reasoning_content from every call this turn.
	reasoning reasoningLog
	// lastReasoning is the most recent call's reasoning, echoed on its assistant turn.
	lastReasoning string
	// liveReasoning streams reasoning deltas as they arrive (see SetReasoningStream).
	liveReasoning reasoningRelay
	final         chatCompletionsFinal
	usageLog
}

// reasoningChatSender issues one request, streaming when onText is non-nil, and returns the
// response plus its reasoning_content. onReasoning, when non-nil, gets streamed reasoning live.
type reasoningChatSender func(ctx context.Context, params openai.ChatCompletionNewParams, onText, onReasoning func(string)) (*openai.ChatCompletion, string, error)

// chatCompletionsWithReasoning is a reasoningChatSender over the shared Chat Completions helpers.
func chatCompletionsWithReasoning(tel *telemetry.Telemetry, dependency string, client *openai.Client) reasoningChatSender {
	return func(ctx context.Context, params openai.ChatCompletionNewParams, onText, onReasoning func(string)) (*openai.ChatCompletion, string, error) {
		if onText == nil {
			resp, err := chatCompletionsNew(ctx, tel, dependency, client, params)
			if err != nil {
				return nil, "", err
			}
			return resp, ChatCompletionReasoning(resp), nil
		}
		var reasoning strings.Builder
		resp, err := chatCompletionsStream(ctx, tel, dependency, client, params, chatCompletionStreamHooks{
			onTextDelta: onText,
			onReasoningDelta: func(d string) {
				reasoning.WriteString(d)
				if onReasoning != nil {
					onReasoning(d)
				}
			},
		})
		if err != nil {
			return nil, "", err
		}
		return resp, reasoning.String(), nil
	}
}

func newReasoningChatAdapter(name string, safety models.SafetyViolationProvider, send reasoningChatSender, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) reasoningChatAdapter {
	tools := chatCompletionScopedTools(&params, functionTools, disabledTools)
	return reasoningChatAdapter{name: name, safety: safety, send: send, replay: reasoningContentReplay{}, params: params, tools: tools}
}

func (a *reasoningChatAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	resp, err := a.call(ctx)
	if err != nil {
		return nil, nil, WrapSafetyViolationError(a.safety, fmt.Errorf("%s API call failed: %w", a.name, err))
	}
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("%s API returned no choices", a.name)
	}
	toolUses := extractChatCompletionToolUses(resp)
	if len(toolUses) == 0 {
		a.final.last = resp
		return a.toGenerateResponse(resp), nil, nil
	}
	a.params.Messages = append(a.params.Messages, a.replay.toolTurn(resp.Choices[0].Message, a.lastReasoning))
	return nil, toolUses, nil
}

func (a *reasoningChatAdapter) AppendToolResults(results []ToolResult) {
	for _, r := range results {
		a.params.Messages = append(a.params.Messages, openai.ToolMessage(toolResultOutput(r), r.ID))
	}
}

func (a *reasoningChatAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	a.params.Tools = nil
	a.params.Messages = append(a.params.Messages, openai.UserMessage("Please provide your best final response based on the information gathered so far without additional tool calls."))
	resp, err := a.call(ctx)
	if err != nil {
		return nil, WrapSafetyViolationError(a.safety, fmt.Errorf("%s final-response call failed: %w", a.name, err))
	}
	a.final.last = resp
	return a.toGenerateResponse(resp), nil
}

// call issues one request and records its usage and reasoning.
func (a *reasoningChatAdapter) call(ctx context.Context) (*openai.ChatCompletion, error) {
	a.liveReasoning.beginCall()
	var onReasoning func(string)
	if a.liveReasoning.enabled() {
		onReasoning = a.liveReasoning.delta
	}
	resp, reasoning, err := a.send(ctx, a.params, a.textDeltaHandler, onReasoning)
	if err != nil {
		return nil, err
	}
	a.addUsage(chatCompletionUsage(resp))
	a.reasoning.add(reasoning)
	a.lastReasoning = reasoning
	return resp, nil
}

// ContinueWith implements Continuer, echoing the reply's reasoning.
func (a *reasoningChatAdapter) ContinueWith(text string) {
	a.final.continueWithReplay(&a.params.Messages, text, a.lastReasoning, a.replay)
}

// SetReasoningStream streams reasoning_content deltas live on streaming calls. Implements
// ReasoningStreamer.
func (a *reasoningChatAdapter) SetReasoningStream(stream ReasoningStream) {
	a.liveReasoning = reasoningRelay{stream: stream, kept: &a.reasoning}
}

func (a *reasoningChatAdapter) WebSearchCompletedCount() int { return 0 }

// SetTextDeltaHandler stores the handler; when set, calls stream token deltas.
func (a *reasoningChatAdapter) SetTextDeltaHandler(handler func(delta string)) {
	a.textDeltaHandler = handler
}

func (a *reasoningChatAdapter) toGenerateResponse(resp *openai.ChatCompletion) *GenerateResponse {
	inputTokens, outputTokens := chatCompletionTokenUsage(resp)
	return &GenerateResponse{
		ID:           resp.ID,
		Text:         ExtractChatCompletionText(resp),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		StopReason:   chatCompletionFinishReason(resp),
		Reasoning:    a.reasoning.String(),
	}
}

// ScopeTools implements ToolScoper.
func (a *reasoningChatAdapter) ScopeTools(hidden func(name string) bool) {
	a.params.Tools = a.tools.scope(hidden)
}
