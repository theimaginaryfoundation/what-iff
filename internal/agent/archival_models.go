package agent

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

// archivalOpenAIModel is the memory role's built-in model (utilityBuiltIn): scratchpad update,
// memory extraction, and checkpoint conversation summary, on every checkpoint path.
// Persona fields archival_model and custom memory/scratchpad prompts are deprecated and ignored.
const archivalOpenAIModel = "gpt-5.6-luna"

// checkpointChainsResponses reports whether a chat's checkpoint can chain off the stored
// OpenAI response rather than rebuild its context from the DB: only when the chat is on
// OpenAI's own Responses API and the memory role's model is too, since a stored conversation
// can only be continued there. Settings that can't be read leave memory on its built-in
// model, which is on that API, so a failed read keeps chaining rather than turning it off.
func (a *Agent) checkpointChainsResponses(ctx context.Context, chatCtx *chatContext) bool {
	if checkpointRebuildsFromDB(chatCtx) {
		return false
	}
	t, chosen := a.chosenUtilityTarget(ctx, provider.UtilityRoleMemory)
	return !chosen || t.usesResponsesAPI()
}
