package provider

import (
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// DefaultQwenBaseURL is DashScope's international OpenAI-compatible endpoint.
// China (Beijing) keys require QWEN_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1.
const DefaultQwenBaseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"

type QwenProvider struct {
	client *openai.Client
	tel    *telemetry.Telemetry
}

// QwenAdapter implements AgentAdapter for Qwen's OpenAI-compatible Chat Completions API. When a
// text-delta handler is set the turn streams token deltas; otherwise it falls back to a single
// non-streaming request. It captures the model's reasoning_content and echoes it back on the
// assistant turns it belongs to (reasoningChatAdapter).
type QwenAdapter struct {
	reasoningChatAdapter
}

func NewQwenProvider(apiKey, baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *QwenProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultQwenBaseURL
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return &QwenProvider{client: &client, tel: tel}
}

func NewQwenAdapter(provider *QwenProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *QwenAdapter {
	send := chatCompletionsWithReasoning(provider.tel, telemetry.DependencyQwen, provider.client)
	return &QwenAdapter{newReasoningChatAdapter("Qwen", models.SafetyViolationProviderQwen, send, params, functionTools, disabledTools)}
}
