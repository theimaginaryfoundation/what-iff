package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// UserPreference holds the schema definition for the UserPreference entity.
type UserPreference struct {
	ent.Schema
}

// Fields of the UserPreference.
func (UserPreference) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("default_model", uuid.UUID{}).
			Comment("The default model to use for the user's new chats"),
		field.UUID("default_personality", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("The default personality to use for the user's new chats"),
		field.Enum("theme").
			Values("light", "dark", "system").
			Default("dark").
			Comment("The theme to use for the user's interface"),
		field.String("last_seen_announcement").
			Optional().
			Default("").
			Comment("ID of the most recently seen announcement banner; empty means none seen"),
		field.Bool("experimental_memory_dedupe_chain").
			Default(false).
			Comment("Deprecated unused column; memory merge/dedupe is always on. Kept until a follow-up migration drops it."),
		field.JSON("favorite_model_ids", []string{}).
			Default([]string{}).
			Comment("Model IDs the user has starred in the model picker, user-global (not per-personality).").
			Annotations(entsql.Default("[]")),
	}
}

// Edges of the UserPreference.
func (UserPreference) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("preferences").
			Unique().
			Required(),
		edge.To("model", Model.Type).
			Field("default_model").
			Unique().
			Required(),
		edge.To("personality", Personality.Type).
			Field("default_personality").
			Unique(),
	}
}
