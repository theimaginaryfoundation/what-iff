package models

// Action type constants matching the Ent usage-event schema enum values. These
// name the action kinds the agent tags when it reports usage through
// the metering seam (internal/metering). They are part of the open-source
// contract even though usage recording itself is a no-op without a linked
// metering implementation, so they live in the public tree.
const (
	ActionTypeChatMessage     = "chat_message"
	ActionTypeJobRun          = "job_run"
	ActionTypeImageGeneration = "image_generation"
	ActionTypeWebSearch       = "web_search"
)
