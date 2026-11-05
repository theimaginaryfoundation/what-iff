package agent

import (
	"context"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func (a *Agent) generateChatName(ctx context.Context, userMessage string) (string, error) {
	// Mock/local mode: deterministic fake — no provider call.
	if a.nonVendorLLM() {
		return mockChatName(userMessage), nil
	}
	name, err := a.CallUtility(ctx, provider.UtilityRequest{
		Role:            provider.UtilityRoleQuick,
		CallPath:        telemetry.CallPathChatName,
		Instructions:    chatNamePrompt,
		Input:           userMessage,
		MaxOutputTokens: provider.DefaultMaxContentLength,
	})
	if err != nil {
		a.logger.Error("failed to generate chat name", zap.Error(err))
		return "", err
	}

	return name, nil
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
