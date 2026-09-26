package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/tidwall/gjson"
)

// DefaultGeminiBaseURL is Google's OpenAI-compatible Chat Completions endpoint.
// Gemini does not speak the OpenAI Responses API used elsewhere in this app, so
// the Gemini path has its own provider/adapter/renderer built on Chat Completions.
const DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"

// GeminiProvider wraps Google's OpenAI-compatible Chat Completions API. It reuses
// the openai-go SDK client pointed at Google's base URL. Only a small surface of
// the Chat Completions API is used (messages, tools, usage).
type GeminiProvider struct {
	client       *openai.Client
	tokenCounter *TokenCounter
	tel          *telemetry.Telemetry
}

// NewGeminiProvider creates a GeminiProvider backed by the given API key. baseURL
// defaults to DefaultGeminiBaseURL when empty. httpClient overrides the SDK's
// default HTTP client when non-nil (deny-network client under MOCK_LLM).
func NewGeminiProvider(apiKey, baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *GeminiProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultGeminiBaseURL
	}
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(2),
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return &GeminiProvider{
		client:       &client,
		tokenCounter: NewTokenCounter(),
		tel:          tel,
	}
}

// completionsNew is the single entry point for Gemini Chat Completions HTTP calls; it
// records the call's duration and token-usage metrics.
func (c *GeminiProvider) completionsNew(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return chatCompletionsNew(ctx, c.tel, telemetry.DependencyGemini, c.client, params)
}

// geminiCompletionTokenUsage is an alias for chatCompletionTokenUsage (defined in
// chat_completions_stream.go, shared across providers) kept for Gemini call sites.
func geminiCompletionTokenUsage(resp *openai.ChatCompletion) (promptTokens, completionTokens int64) {
	return chatCompletionTokenUsage(resp)
}

// Call sends a single Chat Completions request. Retry handling (429/5xx) is
// delegated to the openai-go SDK via WithMaxRetries in NewGeminiProvider.
func (c *GeminiProvider) Call(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return c.completionsNew(ctx, params)
}

// CallStreaming streams a Chat Completions request, forwarding text deltas to
// onTextDelta and recording token-usage metrics on success (mirroring the
// non-streaming completionsNew path).
//
// It also returns the per-tool-call thought signatures (keyed by the streamed
// tool-call index) recovered from the raw deltas. The accumulator drops Gemini's
// extra_content.google.thought_signature, but Google 400s a follow-up request
// whose assistant tool-call turn omits it ("Function call is missing a
// thought_signature"), so the adapter must re-attach it on replay. Empty when the
// turn made no tool calls.
//
// Keying by the streamed tool-call index relies on that index matching the
// position of the same tool call in the accumulated message's ToolCalls slice —
// true for the contiguous 0-based indices Google emits, and how the accumulator
// itself groups deltas. The re-attach site tolerates a missing key (no signature
// added), so a sparse/reordered index degrades to the pre-fix behaviour rather
// than mis-associating.
func (c *GeminiProvider) CallStreaming(ctx context.Context, params openai.ChatCompletionNewParams, onTextDelta func(delta string)) (*openai.ChatCompletion, map[int64]string, error) {
	thoughtSignatures := map[int64]string{}
	resp, err := chatCompletionsStream(ctx, c.tel, telemetry.DependencyGemini, c.client, params, chatCompletionStreamHooks{
		onTextDelta: onTextDelta,
		onToolCallDelta: func(index int64, raw string) {
			if _, seen := thoughtSignatures[index]; seen {
				return
			}
			if sig := gjson.Get(raw, "extra_content.google.thought_signature").String(); sig != "" {
				thoughtSignatures[index] = sig
			}
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, thoughtSignatures, nil
}

// ToGenerateResponse converts a Chat Completion into the provider-agnostic type.
func (c *GeminiProvider) ToGenerateResponse(resp *openai.ChatCompletion) *GenerateResponse {
	if resp == nil {
		return &GenerateResponse{}
	}
	var inputTokens, outputTokens int64
	inputTokens, outputTokens = geminiCompletionTokenUsage(resp)
	return &GenerateResponse{
		ID:           resp.ID,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CreatedAt:    resp.Created,
		Text:         ExtractChatCompletionText(resp),
	}
}

// ExtractChatCompletionText returns the assistant text from the first Chat Completions choice.
func ExtractChatCompletionText(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

// ExtractGeminiText is an alias for ExtractChatCompletionText kept for Gemini call sites.
func ExtractGeminiText(resp *openai.ChatCompletion) string {
	return ExtractChatCompletionText(resp)
}

// CountTokens returns an approximate token count using the shared cl100k_base
// tokenizer. This is a best-effort approximation for Gemini (which uses its own
// tokenizer) but is accurate enough for checkpointing heuristics.
func (c *GeminiProvider) CountTokens(text string) (int, error) {
	return c.tokenCounter.CountTokens(text)
}

// SelectCarryOverTurns delegates to the shared TokenCounter.
func (c *GeminiProvider) SelectCarryOverTurns(recent []*models.ChatMessage, maxTurns, maxTokens int) [][2]*models.ChatMessage {
	return c.tokenCounter.SelectCarryOverTurns(recent, maxTurns, maxTokens)
}
