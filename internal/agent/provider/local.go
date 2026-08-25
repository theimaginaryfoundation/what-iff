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

// DefaultLocalBaseURL is Ollama's OpenAI-compatible endpoint — the reference
// local server this adapter targets, though any OpenAI-compatible Chat
// Completions server works via LOCAL_LLM_BASE_URL.
const DefaultLocalBaseURL = "http://localhost:11434/v1"

// LocalProvider talks to a local OpenAI-compatible Chat Completions server
// (LLM_BACKEND=local). Only reachable under an explicitly-set local/test ENV
// (Config.IsExplicitLocalEnv) — never wired in a production build.
type LocalProvider struct {
	client *openai.Client
	tel    *telemetry.Telemetry
}

// LocalAdapter implements AgentAdapter for a local OpenAI-compatible Chat
// Completions server. Non-streaming, same as the Mistral/DeepSeek/Qwen/Xiaomi
// paths it mirrors — the final assistant text is returned via GenerateResponse.
type LocalAdapter struct {
	provider         *LocalProvider
	params           openai.ChatCompletionNewParams
	textDeltaHandler func(delta string)
}

// NewLocalProvider constructs a client for a local OpenAI-compatible server.
// The API key is a placeholder — local servers such as Ollama and LM Studio
// ignore it, but the SDK requires a non-empty value.
func NewLocalProvider(baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *LocalProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultLocalBaseURL
	}
	opts := []option.RequestOption{
		option.WithAPIKey("local"),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(2),
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return &LocalProvider{client: &client, tel: tel}
}

func (p *LocalProvider) Call(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	recordChatCompletionUsage(ctx, p.tel, resp)
	return resp, nil
}

func NewLocalAdapter(provider *LocalProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *LocalAdapter {
	for _, t := range functionTools {
		if t.OfFunction != nil && disabledTools[t.OfFunction.Function.Name] {
			continue
		}
		params.Tools = append(params.Tools, t)
	}
	return &LocalAdapter{provider: provider, params: params}
}

func (a *LocalAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	resp, err := a.provider.Call(ctx, a.params)
	if err != nil {
		return nil, nil, WrapSafetyViolationError(models.SafetyViolationProviderLocal, fmt.Errorf("local model API call failed: %w", err))
	}
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("local model API returned no choices")
	}
	toolUses := extractChatCompletionToolUses(resp)
	if len(toolUses) == 0 {
		return a.toGenerateResponse(resp), nil, nil
	}
	a.params.Messages = append(a.params.Messages, resp.Choices[0].Message.ToParam())
	return nil, toolUses, nil
}

func (a *LocalAdapter) AppendToolResults(results []ToolResult) {
	for _, r := range results {
		a.params.Messages = append(a.params.Messages, openai.ToolMessage(toolResultOutput(r), r.ID))
	}
}

func (a *LocalAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	a.params.Tools = nil
	a.params.Messages = append(a.params.Messages, openai.UserMessage("Please provide your best final response based on the information gathered so far without additional tool calls."))
	resp, err := a.provider.Call(ctx, a.params)
	if err != nil {
		return nil, WrapSafetyViolationError(models.SafetyViolationProviderLocal, fmt.Errorf("local model final-response call failed: %w", err))
	}
	return a.toGenerateResponse(resp), nil
}

func (a *LocalAdapter) WebSearchCompletedCount() int { return 0 }

// SetTextDeltaHandler stores the handler; the local model path is non-streaming for now.
func (a *LocalAdapter) SetTextDeltaHandler(handler func(delta string)) {
	a.textDeltaHandler = handler
}

func (a *LocalAdapter) toGenerateResponse(resp *openai.ChatCompletion) *GenerateResponse {
	inputTokens, outputTokens := chatCompletionTokenUsage(resp)
	return &GenerateResponse{
		ID:           resp.ID,
		Text:         ExtractChatCompletionText(resp),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
}
