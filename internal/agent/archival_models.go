package agent

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

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
//
// A model chosen for the memory role, by the account or the deployment, replaces this rule
// (rebuiltCheckpointTarget).
func rebuiltCheckpointModel(chatCtx *chatContext) string {
	if chatCtx != nil && chatCtx.connection == nil && models.IsNativeAnthropicModel(chatCtx.modelProvider, chatCtx.model) {
		return archivalClaudeModel
	}
	return archivalOpenAIModel
}

// rebuiltCheckpointTarget is the model for a rebuilt-context checkpoint's scratchpad update
// and memory extraction: the one chosen for the memory role, or else rebuiltCheckpointModel.
func (a *Agent) rebuiltCheckpointTarget(ctx context.Context, chatCtx *chatContext) (utilityTarget, error) {
	t, chosen, err := a.chosenUtilityTarget(ctx, provider.UtilityRoleMemory)
	if err != nil || chosen {
		return t, err
	}
	return utilityTarget{name: rebuiltCheckpointModel(chatCtx)}, nil
}

// checkpointChainsResponses reports whether a chat's checkpoint can chain off the stored
// OpenAI response rather than rebuild its context from the DB: only when the chat is on
// OpenAI's own Responses API and the memory role's model is too, since a stored conversation
// can only be continued there.
func (a *Agent) checkpointChainsResponses(ctx context.Context, chatCtx *chatContext) bool {
	if checkpointRebuildsFromDB(chatCtx) {
		return false
	}
	t, chosen, err := a.chosenUtilityTarget(ctx, provider.UtilityRoleMemory)
	if err != nil {
		// The memory calls fail on either path; rebuilding asks nothing of the chat.
		return false
	}
	return !chosen || t.usesResponsesAPI()
}
