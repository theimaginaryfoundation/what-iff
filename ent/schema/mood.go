package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Mood holds the schema definition for the Mood entity.
// A Mood bundles a name, description, prompt snippet, attached image, and attached
// rituals (skills) into a reusable "mood" for an agent personality.
type Mood struct {
	ent.Schema
}

// Fields of the Mood.
func (Mood) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			Comment("Display name of the mood"),
		field.Text("description").
			Default("").
			Comment("Human-readable description of what this mood does"),
		field.Text("prompt_snippet").
			Default("").
			Comment("Optional prompt text injected when this mood is active"),
		field.Bytes("thumbnail_data").
			Optional().
			Comment("JPEG portrait (typically up to ~128×128) stored in PSQL for fast retrieval; auto-generated from the attached image"),
		field.String("recommended_model").
			Optional().
			Default("").
			Comment("Optional model name to switch to when this mood becomes active; empty means no automatic model change"),
	}
}

// Edges of the Mood.
func (Mood) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("moods").
			Unique().
			Required(),
		// M2M edge retained for compatibility; app-level validation enforces at most one image per mood.
		edge.To("images", FileAttachment.Type),
		// M2M: mood ↔ rituals (the mood's attached skills)
		edge.To("rituals", Ritual.Type),
		// Back-reference: personalities that have attached this mood
		edge.From("personalities", Personality.Type).
			Ref("moods"),
	}
}

// Mixin of the Mood.
func (Mood) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
