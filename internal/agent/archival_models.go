package agent

// archivalOpenAIModel is the memory role's built-in model (utilityBuiltIn): scratchpad update,
// memory extraction, and checkpoint conversation summary, on every checkpoint path.
// Persona fields archival_model and custom memory/scratchpad prompts are deprecated and ignored.
const archivalOpenAIModel = "gpt-5.6-luna"

// checkpointChainsResponses reports whether a chat's checkpoint can chain off the stored
// OpenAI response rather than rebuild its context from the DB: only when the chat is on
// OpenAI's own Responses API and memory, the checkpoint's model, is too, since a stored
// conversation can only be continued there. The caller pins memory for the whole checkpoint
// (withUtilityTarget), so the steps that continue the response run on the model that decided.
func checkpointChainsResponses(chatCtx *chatContext, memory utilityTarget) bool {
	return !checkpointRebuildsFromDB(chatCtx) && memory.usesResponsesAPI()
}
