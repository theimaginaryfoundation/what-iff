package models

// PromptDefaults captures the default prompt text for scratchpad and memory overrides.
type PromptDefaults struct {
	ScratchpadUpdatePrompt string `json:"scratchpad_update_prompt"`
	MemoryQueryPrompt      string `json:"memory_query_prompt"`
	MemoryExtractionPrompt string `json:"memory_extraction_prompt"`
}
