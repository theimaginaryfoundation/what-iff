package provider

import (
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

const DefaultDeepSeekBaseURL = "https://api.deepseek.com/v1"

type DeepSeekProvider struct {
	client *openai.Client
	tel    *telemetry.Telemetry
}

// DeepSeekAdapter implements AgentAdapter for DeepSeek's OpenAI-compatible Chat Completions API. When a
// text-delta handler is set the turn streams token deltas; otherwise it falls back to a single
// non-streaming request. It captures the model's reasoning_content and echoes it back on the
// assistant turns it belongs to (reasoningChatAdapter).
type DeepSeekAdapter struct {
	reasoningChatAdapter
}

func NewDeepSeekProvider(apiKey, baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *DeepSeekProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultDeepSeekBaseURL
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return &DeepSeekProvider{client: &client, tel: tel}
}

func NewDeepSeekAdapter(provider *DeepSeekProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *DeepSeekAdapter {
	send := chatCompletionsWithReasoning(provider.tel, telemetry.DependencyDeepSeek, provider.client)
	return &DeepSeekAdapter{newReasoningChatAdapter("DeepSeek", models.SafetyViolationProviderDeepSeek, send, params, functionTools, disabledTools)}
}
