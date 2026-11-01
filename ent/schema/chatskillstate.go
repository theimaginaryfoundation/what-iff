package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ChatSkillState records a skill (ritual) the agent loaded in one chat with load_skill
// (ADR 0x035 §4). Unloading sets unloaded_at instead of deleting the row, so the row also says the
// skill was used earlier in the thread, which the skills index marks without reading history. Message activations (/skill, hotkeys, the API), mode skills and job skills are
// never written here: they are active for their turn only. A row for a skill that was since
// deleted or moved out of the chat's scope is ignored and dropped by the next unload sweep.
type ChatSkillState struct {
	ent.Schema
}

// Fields of the ChatSkillState.
func (ChatSkillState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("chat_id", uuid.UUID{}),
		field.UUID("ritual_id", uuid.UUID{}),
		field.Time("loaded_at").
			Default(time.Now).
			Comment("When load_skill last loaded the skill; loaded skills are listed in this order"),
		field.Time("unloaded_at").
			Optional().
			Nillable().
			Comment("When unload_skill or a checkpoint unloaded the skill; null while it is loaded"),
	}
}

// Indexes of the ChatSkillState.
func (ChatSkillState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chat_id", "ritual_id").Unique(),
		index.Fields("chat_id"),
	}
}

// Edges of the ChatSkillState.
func (ChatSkillState) Edges() []ent.Edge {
	return nil
}

// Mixin of the ChatSkillState.
func (ChatSkillState) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
