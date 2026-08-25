package agent

import (
	"context"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// TestNewAgent_ThreadsDenyClientIntoProviders proves the no-egress wiring the
// deny transport depends on: an Agent constructed with
// AgentConfig.HTTPClient = DenyNetworkHTTPClient() must produce providers
// whose calls fail with ErrNetworkDenied before any request leaves the
// process — for the shared OpenAI client and the Anthropic client alike.
func TestNewAgent_ThreadsDenyClientIntoProviders(t *testing.T) {
	t.Parallel()

	a := NewAgent(
		nil,
		zap.NewNop(),
		telemetry.LoggerOnly(zap.NewNop()),
		"dummy-key",
		nil,
		"dummy-anthropic-key",
		AgentConfig{
			HTTPClient: provider.DenyNetworkHTTPClient(),
			LLMBackend: "mock",
		},
	)

	_, err := a.OpenAIProvider.CallWithRetry(context.Background(), responses.ResponseNewParams{
		Model: "gpt-4o-mini",
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("hi")},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrNetworkDenied, "shared OpenAI client must be egress-denied")

	require.NotNil(t, a.ClaudeProvider)
	_, err = a.ClaudeProvider.Call(context.Background(), anthropic.MessageNewParams{
		Model:     "claude-haiku-4-5",
		MaxTokens: 8,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrNetworkDenied, "Anthropic client must be egress-denied")
}
