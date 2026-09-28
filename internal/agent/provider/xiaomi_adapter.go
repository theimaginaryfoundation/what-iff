package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// DefaultXiaomiBaseURL is Xiaomi MiMo's pay-as-you-go OpenAI-compatible endpoint.
// Token Plan subscriptions use a region-specific base URL (set XIAOMI_BASE_URL).
const DefaultXiaomiBaseURL = "https://api.xiaomimimo.com/v1"

type XiaomiProvider struct {
	client *openai.Client
	tel    *telemetry.Telemetry
}

// XiaomiAdapter implements AgentAdapter for Xiaomi MiMo's OpenAI-compatible Chat
// Completions API. When a text-delta handler is set the turn streams token
// deltas; otherwise it falls back to a single non-streaming request. The full
// assistant text is always available via GenerateResponse.
//
// Because MiMo always reasons and has no reasoning budget, a call that is cut off at
// the length limit before any reply text is transparently re-issued once with
// thinking disabled (see call); a second truncation is returned as-is, with
// StopReason "length", for the agent's empty-turn guard to report.
type XiaomiAdapter struct {
	provider         *XiaomiProvider
	params           openai.ChatCompletionNewParams
	textDeltaHandler func(delta string)

	// reasoning collects reasoning_content from every call this turn.
	reasoning reasoningLog
	// lastReasoning is the most recent call's reasoning_content, echoed back on the
	// assistant tool-call message it belongs to (MiMo 400s without it; see
	// chatCompletionAssistantReplay).
	lastReasoning string
	// liveReasoning streams reasoning_content deltas as they arrive (see SetReasoningStream).
	liveReasoning reasoningRelay
	// thinkingDisabled records that a truncated call already triggered the
	// thinking-off retry, so a turn retries at most once and later rounds stay off.
	thinkingDisabled bool
}

func NewXiaomiProvider(apiKey, baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *XiaomiProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultXiaomiBaseURL
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return &XiaomiProvider{client: &client, tel: tel}
}

// Call issues a non-streaming request and returns the response plus its
// reasoning_content (MiMo always reasons unless thinking is disabled).
func (p *XiaomiProvider) Call(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, string, error) {
	resp, err := chatCompletionsNew(ctx, p.tel, telemetry.DependencyXiaomi, p.client, params)
	if err != nil {
		return nil, "", err
	}
	return resp, ChatCompletionReasoning(resp), nil
}

// CallStreaming streams a Chat Completions request, forwarding text deltas to
// onTextDelta, and returns the response plus the streamed reasoning_content.
// onReasoningDelta, when non-nil, additionally receives each reasoning chunk live.
// (The SDK only retries before the response body starts, so a stream is never
// replayed and live reasoning cannot be duplicated by a transport retry.)
func (p *XiaomiProvider) CallStreaming(ctx context.Context, params openai.ChatCompletionNewParams, onTextDelta func(delta string), onReasoningDelta func(delta string)) (*openai.ChatCompletion, string, error) {
	var reasoning strings.Builder
	resp, err := chatCompletionsStream(ctx, p.tel, telemetry.DependencyXiaomi, p.client, params, chatCompletionStreamHooks{
		onTextDelta: onTextDelta,
		onReasoningDelta: func(d string) {
			reasoning.WriteString(d)
			if onReasoningDelta != nil {
				onReasoningDelta(d)
			}
		},
	})
	if err != nil {
		return nil, "", err
	}
	return resp, reasoning.String(), nil
}

// recordRetry counts an adapter-level fallback (the thinking-off length retry) for model.
func (p *XiaomiProvider) recordRetry(ctx context.Context, model, reason string) {
	var tel *telemetry.Telemetry
	if p != nil {
		tel = p.tel
	}
	startGenAICall(ctx, tel, telemetry.DependencyXiaomi, model, genAIOpChat).retry(reason)
}

func NewXiaomiAdapter(provider *XiaomiProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *XiaomiAdapter {
	for _, t := range functionTools {
		if t.OfFunction != nil && disabledTools[t.OfFunction.Function.Name] {
			continue
		}
		params.Tools = append(params.Tools, t)
	}
	// MiMo reasons by default and bills reasoning against the same cap as the answer,
	// with no budget knob — give it the raised reasoning-model cap. Only the generic
	// default (or no cap) is raised; a caller that chose some other cap keeps it.
	if limit := params.MaxCompletionTokens; !limit.Valid() || limit.Value == DefaultMaxContentLength {
		params.MaxCompletionTokens = openai.Int(ReasoningMaxOutputTokens)
	}
	return &XiaomiAdapter{provider: provider, params: params}
}

func (a *XiaomiAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	resp, err := a.call(ctx)
	if err != nil {
		return nil, nil, WrapSafetyViolationError(models.SafetyViolationProviderXiaomi, fmt.Errorf("Xiaomi API call failed: %w", err))
	}
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("Xiaomi API returned no choices")
	}
	toolUses := extractChatCompletionToolUses(resp)
	if len(toolUses) == 0 {
		return a.toGenerateResponse(resp), nil, nil
	}
	a.params.Messages = append(a.params.Messages, chatCompletionAssistantReplay(resp.Choices[0].Message, a.lastReasoning))
	return nil, toolUses, nil
}

func (a *XiaomiAdapter) AppendToolResults(results []ToolResult) {
	for _, r := range results {
		a.params.Messages = append(a.params.Messages, openai.ToolMessage(toolResultOutput(r), r.ID))
	}
}

func (a *XiaomiAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	a.params.Tools = nil
	a.params.Messages = append(a.params.Messages, openai.UserMessage("Please provide your best final response based on the information gathered so far without additional tool calls."))
	resp, err := a.call(ctx)
	if err != nil {
		return nil, WrapSafetyViolationError(models.SafetyViolationProviderXiaomi, fmt.Errorf("Xiaomi final-response call failed: %w", err))
	}
	return a.toGenerateResponse(resp), nil
}

// call issues one request and records its reasoning. MiMo has no reasoning budget, so
// when a response is cut off at the length limit before any reply text (the whole cap
// went to reasoning), it is discarded — no text deltas were streamed — and re-issued
// once with thinking disabled; thinking then stays off for the rest of the turn.
func (a *XiaomiAdapter) call(ctx context.Context) (*openai.ChatCompletion, error) {
	resp, reasoning, err := a.callOnce(ctx)
	if err == nil && !a.thinkingDisabled && chatCompletionTruncatedWithoutText(resp) {
		a.provider.recordRetry(ctx, string(a.params.Model), retryReasonLength)
		a.thinkingDisabled = true
		a.params.SetExtraFields(map[string]any{"thinking": map[string]any{"type": "disabled"}})
		a.liveReasoning.reset()
		resp, reasoning, err = a.callOnce(ctx)
	}
	if err != nil {
		return nil, err
	}
	a.reasoning.add(reasoning)
	a.lastReasoning = reasoning
	return resp, nil
}

// callOnce streams when a text-delta handler is set, else issues a non-streaming request.
func (a *XiaomiAdapter) callOnce(ctx context.Context) (*openai.ChatCompletion, string, error) {
	a.liveReasoning.beginCall()
	if a.textDeltaHandler != nil {
		var onReasoning func(string)
		if a.liveReasoning.enabled() {
			onReasoning = a.liveReasoning.delta
		}
		return a.provider.CallStreaming(ctx, a.params, a.textDeltaHandler, onReasoning)
	}
	return a.provider.Call(ctx, a.params)
}

// SetReasoningStream streams reasoning_content deltas live on streaming calls.
// Implements ReasoningStreamer.
func (a *XiaomiAdapter) SetReasoningStream(stream ReasoningStream) {
	a.liveReasoning = reasoningRelay{stream: stream, kept: &a.reasoning}
}

func (a *XiaomiAdapter) WebSearchCompletedCount() int { return 0 }

// SetTextDeltaHandler stores the handler; when set, the Xiaomi path streams token deltas.
func (a *XiaomiAdapter) SetTextDeltaHandler(handler func(delta string)) { a.textDeltaHandler = handler }

func (a *XiaomiAdapter) toGenerateResponse(resp *openai.ChatCompletion) *GenerateResponse {
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
