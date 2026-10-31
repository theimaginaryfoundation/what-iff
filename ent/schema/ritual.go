package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Ritual holds the schema definition for the Ritual entity.
type Ritual struct {
	ent.Schema
}

// Fields of the Ritual.
func (Ritual) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			Comment("The name of the ritual"),
		field.Text("description").
			NotEmpty().
			Comment("The description of the ritual"),
		field.Text("content").
			NotEmpty().
			Comment("The content of the ritual"),
		field.String("hotkeys").
			Comment("The keyboard shortcut hotkeys of the ritual"),
	}
}

// Edges of the Ritual.
func (Ritual) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("rituals").
			Unique().
			Required(),
		edge.To("personality", Personality.Type).
			Unique(),
		edge.From("chat_messages", ChatMessage.Type).
			Ref("rituals"),
		edge.To("mcp_servers", MCPServer.Type),
		// Inverse of Mood.rituals and AgentJob.rituals. Declaring the back-reference makes both
		// edges many-to-many (join tables mood_rituals and agent_job_rituals) instead of the
		// one-to-many FK columns ent produced without it, which let a skill sit on only one mode
		// or job at a time (ADR 0x035 §11).
		edge.From("moods", Mood.Type).
			Ref("rituals"),
		edge.From("agent_jobs", AgentJob.Type).
			Ref("rituals"),
	}
}

// Mixin of the Ritual.
func (Ritual) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
