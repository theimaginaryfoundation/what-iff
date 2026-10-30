package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AgentEntity is one page of the agent-written entity wiki (ADR 0x034): a short text body about a
// person, place, project or other noun, looked up by an exact, normalized key. Agents write and
// read these with the entity_write and entity_get tools; the user sees them in the Memories panel.
//
// Scope follows memories: with the writing personality's auto_pin_memories on, the entity is
// pinned to that personality and only it sees the entity; otherwise it is global to the user.
type AgentEntity struct {
	ent.Schema
}

// Fields of the AgentEntity.
func (AgentEntity) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("key").
			NotEmpty().
			MaxLen(200).
			Comment("Normalized lookup key: lowercased, trimmed, inner whitespace collapsed. See models.NormalizeEntityKey."),
		field.String("title").
			NotEmpty().
			MaxLen(200).
			Comment("The key as first written, for display."),
		field.Text("body").
			Default("").
			Comment("Entity text, at most models.MaxEntityBodyRunes runes."),
		field.UUID("pinned_personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Set when the entity belongs to one personality; null for a global entity."),
		field.UUID("last_personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Personality whose agent last wrote the entity; null when written with no personality or by the user."),
		field.UUID("last_chat_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Conversation that last wrote the entity; null when the user edited it."),
	}
}

// Edges of the AgentEntity.
func (AgentEntity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("agent_entities").
			Unique().
			Required(),
		edge.From("pinned_personality", Personality.Type).
			Ref("agent_entities").
			Field("pinned_personality_id").
			Unique(),
	}
}

// Indexes of the AgentEntity.
func (AgentEntity) Indexes() []ent.Index {
	return []ent.Index{
		// One global entity per key, and one per key for each personality. Two partial indexes
		// because a plain unique index treats every null pinned_personality_id as distinct.
		index.Fields("key").
			Edges("owner").
			Unique().
			Annotations(entsql.IndexWhere("pinned_personality_id IS NULL")),
		index.Fields("key", "pinned_personality_id").
			Edges("owner").
			Unique().
			Annotations(entsql.IndexWhere("pinned_personality_id IS NOT NULL")),
		index.Edges("owner"),
	}
}

// Mixin of the AgentEntity.
func (AgentEntity) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
