package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// PersonalityGenFlow holds the schema definition for a personality-generation wizard session.
type PersonalityGenFlow struct {
	ent.Schema
}

// Fields of the PersonalityGenFlow.
func (PersonalityGenFlow) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Enum("status").
			Values("in_progress", "generated", "accepted").
			Default("in_progress").
			Comment("Wizard state: in_progress while answering, generated after OpenAI call, accepted once personality is created"),
		field.Int("current_step").
			Default(0).
			Comment("0-indexed page the user is currently on"),
		field.JSON("answers", map[string]string{}).
			Default(map[string]string{}).
			Comment("Question answers keyed by question ID"),
		field.Text("generated_prompt").
			Default("").
			Comment("System prompt produced by OpenAI generation"),
		field.Text("generated_about_me").
			Default("").
			Comment("About-me blurb shown to the user for review"),
		field.JSON("generated_names", []string{}).
			Optional().
			Default([]string{}).
			Comment("Name candidates produced by OpenAI generation"),
		field.String("image_style").
			Default("auto").
			Comment("Image generation style hint (e.g. 'auto', 'anime', 'none'). Copied to Personality on accept."),
	}
}

// Edges of the PersonalityGenFlow.
func (PersonalityGenFlow) Edges() []ent.Edge {
	return []ent.Edge{
		// Unique here means each flow has exactly one owner user.
		// The inverse edge on User allows one user to have many flows over time.
		edge.From("user", User.Type).
			Ref("personality_gen_flows").
			Unique().
			Required(),
		edge.To("personality", Personality.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		// Optional reference image uploaded by the user during the wizard.
		// Becomes the personality cover_image on accept when no explicit cover is chosen.
		edge.To("reference_image", FileAttachment.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

// Indexes of the PersonalityGenFlow.
func (PersonalityGenFlow) Indexes() []ent.Index {
	return []ent.Index{
		// Supports fast lookup of "active" flows by user + status.
		index.Edges("user").Fields("status", "updated_at"),
	}
}

// Mixin of the PersonalityGenFlow.
func (PersonalityGenFlow) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
