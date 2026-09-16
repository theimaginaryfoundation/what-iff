package agent

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// The bug this pins: every one of these providers takes the rebuilt-context
// checkpoint path, and archival used to be hardcoded to Anthropic for all of
// them. A Gemini or GLM account has no reason to hold an Anthropic key, so
// their chats worked while memory silently stopped updating.
func TestArchivalUsesClaude_OnlyForGenuinelyAnthropicChats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		modelProvider string
		modelName     string
		wantClaude    bool
		why           string
	}{
		{
			name:          "anthropic archives on claude",
			modelProvider: string(models.ModelProviderAnthropic),
			modelName:     "claude-sonnet-5",
			wantClaude:    true,
			why:           "chatting with Claude proves the account has an Anthropic key",
		},
		{
			name:          "zai archives on openai",
			modelProvider: string(models.ModelProviderZAI),
			modelName:     "glm-5.3",
			wantClaude:    false,
			why:           "z.ai speaks the Anthropic wire format but bills a different account",
		},
		{
			name:          "gemini archives on openai",
			modelProvider: string(models.ModelProviderGoogle),
			modelName:     "gemini-3.8-flash",
			wantClaude:    false,
			why:           "Gemini reaches this path for lack of a response id, not for Anthropic",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equalf(t, tc.wantClaude, archivalUsesClaude(tc.modelProvider, tc.modelName), tc.why)
		})
	}
}

// Instruction turns have to land after the conversation they are about.
func TestArchivalContextItems_AppendsExtrasAfterContext(t *testing.T) {
	t.Parallel()

	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindHistoryTurn, provider.RoleUser, "earlier turn", false)

	items := archivalContextItems(mc,
		responses.ResponseInputItemParamOfMessage("the instruction", provider.RoleUser),
	)

	require.Len(t, items, 2)
	require.Equal(t, "the instruction", items[len(items)-1].OfMessage.Content.OfString.Value)
}

// A nil context must not panic the checkpoint; it degrades to the instructions.
func TestArchivalContextItems_NilContext(t *testing.T) {
	t.Parallel()

	items := archivalContextItems(nil,
		responses.ResponseInputItemParamOfMessage("the instruction", provider.RoleUser),
	)
	require.Len(t, items, 1)
}
