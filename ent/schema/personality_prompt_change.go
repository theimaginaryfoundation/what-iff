package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PersonalityPromptChange is an append-only audit record for a personality
// system-prompt transition. Reverts append another row instead of modifying the
// historical change they restore.
type PersonalityPromptChange struct {
	ent.Schema
}

func (PersonalityPromptChange) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("user_id", uuid.UUID{}).
			Immutable().
			Comment("Owner of the personality at change time"),
		field.UUID("personality_id", uuid.UUID{}).
			Immutable().
			Comment("Personality whose system prompt changed"),
		field.Text("old_prompt").
			Immutable(),
		field.Text("new_prompt").
			Immutable(),
		field.Enum("action").
			Values("edit", "revert").
			Immutable(),
		field.UUID("reverted_change_id", uuid.UUID{}).
			Optional().
			Nillable().
			Immutable().
			Comment("Original change selected for restoration, when action is revert"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

func (PersonalityPromptChange) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "personality_id", "created_at"),
		index.Fields("personality_id"),
	}
}

func (PersonalityPromptChange) Edges() []ent.Edge {
	return nil
}
