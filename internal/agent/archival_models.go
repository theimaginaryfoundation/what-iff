package agent

import "github.com/theimaginaryfoundation/what-iff/internal/models"

// archivalOpenAIModel and archivalClaudeModel are fixed models for background work:
// scratchpad update, memory extraction, and checkpoint conversation summary.
// Persona fields archival_model and custom memory/scratchpad prompts are deprecated and ignored.
const (
	archivalOpenAIModel = "gpt-5.6-luna"
	archivalClaudeModel = "claude-haiku-4-5"
)

// rebuiltCheckpointModel is the model for a checkpoint's scratchpad update and memory
// extraction when the chat's context is rebuilt from the database rather than chained off a
// stored OpenAI response. A chat on a native Anthropic model keeps a Claude model; every
// other chat (Gemini, Mistral, z.ai, an OpenRouter, local or Custom connection, and so on)
// uses the OpenAI archival model, whose key the account already holds for embeddings,
// rather than needing an Anthropic key it may not have.
func rebuiltCheckpointModel(chatCtx *chatContext) string {
	if chatCtx != nil && chatCtx.connection == nil && models.IsNativeAnthropicModel(chatCtx.modelProvider, chatCtx.model) {
		return archivalClaudeModel
	}
	return archivalOpenAIModel
}
