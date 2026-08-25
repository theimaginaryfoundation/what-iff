package agent

// archivalOpenAIModel and archivalClaudeModel are fixed models for background work:
// scratchpad update, memory extraction, and checkpoint conversation summary.
// Persona fields archival_model and custom memory/scratchpad prompts are deprecated and ignored.
const (
	archivalOpenAIModel = "gpt-5.6-luna"
	archivalClaudeModel = "claude-haiku-4-5"
)
