package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
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
