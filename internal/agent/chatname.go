package agent

import (
	"context"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

const chatNameModel = "gpt-4.1-nano-2025-04-14"

func (a *Agent) generateChatName(ctx context.Context, userMessage string) (string, error) {
	// Mock/local mode: deterministic fake — no provider call.
	if a.nonVendorLLM() {
		return mockChatName(userMessage), nil
	}
	params := responses.ResponseNewParams{
		Model:           chatNameModel,
		Temperature:     openai.Float(provider.DefaultTemperature),
		MaxOutputTokens: openai.Int(provider.DefaultMaxContentLength),
		Instructions:    openai.String(chatNamePrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userMessage),
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathChatName), params)
	if err != nil {
		a.logger.Error("failed to generate chat name", zap.Error(err))
		return "", err
	}

	return resp.OutputText(), nil
}

// mockChatName derives a deterministic chat name from the first words of the
// user message, used under MOCK_LLM instead of the naming model.
func mockChatName(userMessage string) string {
	const maxLen = 40
	name := strings.Join(strings.Fields(userMessage), " ")
	if name == "" {
		return "Mock Chat"
	}
	runes := []rune(name)
	if len(runes) > maxLen {
		name = string(runes[:maxLen]) + "…"
	}
	return name
}
