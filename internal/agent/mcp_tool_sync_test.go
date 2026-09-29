package agent

import (
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"go.uber.org/zap"
)

// Every real adapter must bind a same-turn MCP tool sync; the mock adapter has none.
func TestBindMCPToolSyncCoversEveryAdapter(t *testing.T) {
	params := openai.ChatCompletionNewParams{}
	specs := []agenttools.FunctionToolSpec{{Name: "mcp__abc__search", Description: "search"}}
	for name, adapter := range map[string]provider.AgentAdapter{
		"openai":   provider.NewOpenAIAdapter(nil, responses.ResponseNewParams{}),
		"claude":   provider.NewClaudeAdapter(nil, anthropic.MessageNewParams{}, nil, false, nil, nil),
		"gemini":   provider.NewGeminiAdapter(nil, params, nil, nil, zap.NewNop()),
		"mistral":  provider.NewMistralAdapter(nil, params, nil, nil),
		"deepseek": provider.NewDeepSeekAdapter(nil, params, nil, nil),
		"qwen":     provider.NewQwenAdapter(nil, params, nil, nil),
		"xiaomi":   provider.NewXiaomiAdapter(nil, params, nil, nil),
		"local":    provider.NewLocalAdapter(nil, params, nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			chatCtx := &chatContext{mcpToolsChanged: true}
			bindMCPToolSync(chatCtx, adapter, zap.NewNop())
			require.NotNil(t, chatCtx.syncMCPTools)
			require.False(t, chatCtx.mcpToolsChanged, "binding starts the turn with no pending change")
			require.NotPanics(t, func() { chatCtx.syncMCPTools(specs) })
		})
	}

	chatCtx := &chatContext{syncMCPTools: func([]agenttools.FunctionToolSpec) {}}
	bindMCPToolSync(chatCtx, provider.NewMockAdapter(provider.MockAdapterConfig{}), zap.NewNop())
	require.Nil(t, chatCtx.syncMCPTools)
}
