package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// turnSettingFields are the per-level reasoning effort and agent loop settings (ADR 0x026),
// shared by every level that can set them: account preference, personality, chat, mood and
// agent job. NULL means "not set at this level". Values match models.ReasoningEffort and
// models.AgentLoop.
func turnSettingFields(level string) []ent.Field {
	return []ent.Field{
		field.Enum("reasoning_effort").
			Values("off", "low", "medium", "high").
			Optional().
			Nillable().
			Comment("Reasoning effort set at the " + level + " level (ADR 0x026); NULL means not set."),
		field.Enum("agent_loop").
			Values("standard", "advanced").
			Optional().
			Nillable().
			Comment("Agent loop set at the " + level + " level (ADR 0x026): standard tool loop or advanced ReAct loop; NULL means not set."),
	}
}
