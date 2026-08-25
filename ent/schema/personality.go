package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Personality holds the schema definition for the Personality entity.
type Personality struct {
	ent.Schema
}

// PersonalityThumbnailCircle stores normalized portrait framing metadata used
// for circular thumbnail rendering in clients.
type PersonalityThumbnailCircle struct {
	CX float64 `json:"cx"`
	CY float64 `json:"cy"`
	R  float64 `json:"r"`
}

// Fields of the Personality.
func (Personality) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			Comment("The name of the personality"),
		field.Text("system_prompt").
			NotEmpty().
			Comment("The system prompt for the personality"),
		field.Text("scratchpad").
			Default("").
			Comment("A free-form scratchpad for the personality to use for storing information and customizing it's own prompt and behavior."),
		field.JSON("scratchpad_history", []string{}).
			Default([]string{}).
			Comment("History of previous scratchpad states with the most recent entry first.").
			Annotations(entsql.Default("[]")),
		field.String("archival_model").
			Default("").
			Comment("Deprecated: retained for schema compatibility; ignored by the server."),
		field.Text("scratchpad_update_prompt").
			Default("").
			Comment("Optional custom prompt for scratchpad update during checkpoints; empty uses the app default."),
		field.Text("memory_search_prompt").
			Default("").
			Comment("Deprecated: retained for schema compatibility; ignored by the server."),
		field.Text("memory_write_prompt").
			Default("").
			Comment("Deprecated: retained for schema compatibility; ignored by the server."),
		field.Bool("auto_pin_memories").
			Default(false).
			Comment("When enabled, new User-scoped memories created while this personality is active will be automatically pinned to this personality."),
		field.String("accent_color").
			Optional().
			Nillable().
			Comment("Optional hex color chosen by the user for persona accent styling."),
		field.JSON("thumbnail_circle", &PersonalityThumbnailCircle{}).
			Optional().
			Comment("Optional normalized circle bounds (cx, cy, r) used to focus circular personality thumbnails."),
		field.Bool("expressions_enabled").
			Default(true).
			Comment("When false, expression picking is skipped in chat and expression frames are hidden in the UI."),
		field.String("image_style").
			Default("auto").
			Comment("Preferred image generation style (e.g. 'auto', 'anime', 'none'). Used for portrait and expression grid generation."),
	}
}

// Edges of the Personality.
func (Personality) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("personalities").
			Unique().
			Required(),
		edge.To("file_attachments", FileAttachment.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("expressions", PersonalityExpression.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		// Optional portrait image used as the personality cover in cards and detail headers.
		// Cleared automatically when the underlying gallery image is deleted.
		edge.To("cover_image", FileAttachment.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		// M2M: a personality can have multiple moods as available "moods".
		edge.To("moods", Mood.Type),
	}
}

// Mixin of the Personality.
func (Personality) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
