package provider

import (
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// Clients for a provider connection (ADR 0x027): OpenRouter, a server on this
// computer, or any compatible server, at the address and with the key its
// connection holds.
//
// They are assembled from the SDKs' services rather than with NewClient,
// whose defaults read OPENAI_API_KEY, OPENAI_ORG_ID, ANTHROPIC_API_KEY and
// ANTHROPIC_AUTH_TOKEN from the environment: a connection sends its own key,
// or none, and never the deployment's. The HTTP client should not be the
// shared provider client either, whose credential transport puts the account's
// built-in keys on requests to the built-in providers' hosts.

// ConnectionChatProvider talks to a connection that speaks the OpenAI Chat
// Completions API.
type ConnectionChatProvider struct {
	client     *openai.Client
	tel        *telemetry.Telemetry
	dependency string
	safety     models.SafetyViolationProvider
}

// NewConnectionChatProvider builds a Chat Completions client for a connection.
// dependency labels its calls in telemetry (telemetry.Dependency*), and safety
// names it in a recorded safety refusal.
func NewConnectionChatProvider(key, baseURL, dependency string, safety models.SafetyViolationProvider, tel *telemetry.Telemetry, httpClient *http.Client) *ConnectionChatProvider {
	opts := []option.RequestOption{option.WithBaseURL(baseURL), option.WithMaxRetries(2)}
	if strings.TrimSpace(key) != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.Client{Options: opts, Chat: openai.NewChatService(opts...)}
	return &ConnectionChatProvider{client: &client, tel: tel, dependency: dependency, safety: safety}
}

// ConnectionChatAdapter implements AgentAdapter for a Chat Completions
// connection. It streams, handles tool calls and captures reasoning_content
// where the server sends it, like the built-in Chat Completions providers, but
// sends no reasoning back: a server that does not know the field could refuse
// the request.
type ConnectionChatAdapter struct {
	reasoningChatAdapter
}

// NewConnectionChatAdapter builds the adapter for one turn.
func NewConnectionChatAdapter(p *ConnectionChatProvider, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) *ConnectionChatAdapter {
	send := chatCompletionsWithReasoning(p.tel, p.dependency, p.client)
	core := newReasoningChatAdapter("Provider connection", p.safety, send, params, functionTools, disabledTools)
	core.replay = noReasoningReplay{}
	return &ConnectionChatAdapter{core}
}

// noReasoningReplay replays assistant turns without their reasoning.
type noReasoningReplay struct{}

func (noReasoningReplay) toolTurn(msg openai.ChatCompletionMessage, _ string) openai.ChatCompletionMessageParamUnion {
	return chatCompletionAssistantReplay(msg, "")
}

func (noReasoningReplay) finalTurn(text, _ string) openai.ChatCompletionMessageParamUnion {
	p := openai.ChatCompletionAssistantMessageParam{
		Content: openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(text)},
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &p}
}

// NewConnectionClaudeProvider builds an Anthropic Messages client for a
// connection that speaks that API. dependency labels its calls in telemetry.
func NewConnectionClaudeProvider(key, baseURL, dependency string, tel *telemetry.Telemetry, httpClient *http.Client) *ClaudeProvider {
	opts := []anthropicoption.RequestOption{anthropicoption.WithBaseURL(baseURL), anthropicoption.WithMaxRetries(2)}
	if strings.TrimSpace(key) != "" {
		opts = append(opts, anthropicoption.WithAPIKey(key))
	}
	if httpClient != nil {
		opts = append(opts, anthropicoption.WithHTTPClient(httpClient))
	}
	client := anthropic.Client{
		Options:  opts,
		Messages: anthropic.NewMessageService(opts...),
		Beta:     anthropic.NewBetaService(opts...),
	}
	return &ClaudeProvider{client: &client, tokenCounter: NewTokenCounter(), tel: tel, providerName: dependency}
}
