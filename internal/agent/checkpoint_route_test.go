package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A chat on a connection model checkpoints from the DB, whatever its name or
// preset: none of them returns an OpenAI response id to chain off.
func TestCheckpointRebuildsFromDB_ForConnectionModels(t *testing.T) {
	for _, preset := range []string{"openrouter", "ollama", "lmstudio", "custom"} {
		chatCtx := &chatContext{model: "gpt-5.4", modelProvider: preset, connection: &ConnectionEndpoint{Preset: preset}}
		require.True(t, checkpointRebuildsFromDB(chatCtx), preset)
	}
	require.False(t, checkpointRebuildsFromDB(&chatContext{model: "gpt-5.4", modelProvider: "openai"}), "OpenAI's own Responses chats chain off the response id")
	require.True(t, checkpointRebuildsFromDB(&chatContext{model: "claude-sonnet-4-6", modelProvider: "anthropic"}))
}

// A turn that carried pre/post context checkpoints from the DB on any model, an OpenAI Responses
// one included: its response chain holds the caller's context, and the rebuild strips it.
func TestCheckpointRebuildsFromDB_ForTurnsWithContext(t *testing.T) {
	openAI := func(opts *models.TurnOptions) *chatContext {
		return &chatContext{model: "gpt-5.4", modelProvider: "openai", turnOptions: opts}
	}
	require.True(t, checkpointRebuildsFromDB(openAI(&models.TurnOptions{PreContext: "PRE"})))
	require.True(t, checkpointRebuildsFromDB(openAI(&models.TurnOptions{PostContext: "POST"})))
	require.False(t, checkpointRebuildsFromDB(openAI(&models.TurnOptions{MaxOutputTokens: 800})), "options without context keep the chain")
	require.False(t, checkpointRebuildsFromDB(openAI(&models.TurnOptions{PreContext: "  "})), "blank context adds none")
}
