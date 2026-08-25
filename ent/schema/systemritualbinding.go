package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// SystemRitualBinding holds per-user hotkey bindings for system rituals.
type SystemRitualBinding struct {
	ent.Schema
}

// Fields of the SystemRitualBinding.
func (SystemRitualBinding) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("ritual_id", uuid.UUID{}).
			Comment("UUID of the system ritual (reserved IDs)"),
		field.String("hotkeys").
			Optional().
			Nillable().
			Comment("Per-user hotkey override for the system ritual"),
		// timestamps provided by TimeMixin
	}
}

// Edges of the SystemRitualBinding.
func (SystemRitualBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("system_ritual_bindings").
			Unique().
			Required(),
	}
}

// Indexes of the SystemRitualBinding.
func (SystemRitualBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("user").Fields("ritual_id").Unique(),
		// enforce unique hotkey per-user (NULLs allowed)
		index.Edges("user").Fields("hotkeys").Unique(),
	}
}

// Mixin for timestamps
func (SystemRitualBinding) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
