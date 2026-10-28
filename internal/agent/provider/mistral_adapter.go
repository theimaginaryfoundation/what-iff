package provider

import (
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

const DefaultMistralBaseURL = "https://api.mistral.ai/v1"

type MistralProvider struct {
	client *openai.Client
	tel    *telemetry.Telemetry
}

// MistralAdapter implements AgentAdapter for Mistral's OpenAI-compatible Chat Completions API. When
// a text-delta handler is set the turn streams token deltas; otherwise it falls back to a single
// non-streaming request. Content is read through mistralSender, which splits a reasoning model's
// thinking chunks from its reply, and thinking is replayed as Mistral's own chunks
// (mistralChunkReplay).
type MistralAdapter struct {
	reasoningChatAdapter
}

func NewMistralProvider(apiKey, baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *MistralProvider {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultMistralBaseURL
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return &MistralProvider{client: &client, tel: tel}
}

func NewMistralAdapter(provider *MistralProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *MistralAdapter {
	a := &MistralAdapter{newReasoningChatAdapter("Mistral", models.SafetyViolationProviderMistral, mistralSender(provider.tel, provider.client), params, functionTools, disabledTools)}
	a.replay = mistralChunkReplay{}
	return a
}
