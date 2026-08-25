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
type XiaomiAdapter struct {
	provider         *XiaomiProvider
	params           openai.ChatCompletionNewParams
	textDeltaHandler func(delta string)
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

func (p *XiaomiProvider) Call(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	recordChatCompletionUsage(ctx, p.tel, resp)
	return resp, nil
}

// CallStreaming streams a Chat Completions request, forwarding text deltas to onTextDelta.
func (p *XiaomiProvider) CallStreaming(ctx context.Context, params openai.ChatCompletionNewParams, onTextDelta func(delta string)) (*openai.ChatCompletion, error) {
	resp, err := streamChatCompletion(ctx, p.client, params, onTextDelta)
	if err != nil {
		return nil, err
	}
	recordChatCompletionUsage(ctx, p.tel, resp)
	return resp, nil
}

func NewXiaomiAdapter(provider *XiaomiProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *XiaomiAdapter {
	for _, t := range functionTools {
		if t.OfFunction != nil && disabledTools[t.OfFunction.Function.Name] {
			continue
		}
		params.Tools = append(params.Tools, t)
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
	a.params.Messages = append(a.params.Messages, resp.Choices[0].Message.ToParam())
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

// call streams when a text-delta handler is set, else issues a non-streaming request.
func (a *XiaomiAdapter) call(ctx context.Context) (*openai.ChatCompletion, error) {
	if a.textDeltaHandler != nil {
		return a.provider.CallStreaming(ctx, a.params, a.textDeltaHandler)
	}
	return a.provider.Call(ctx, a.params)
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
	}
}
