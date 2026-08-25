package schema

import (
	"regexp"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

var expressionKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// PersonalityExpression holds an assignable expression slot for a personality.
type PersonalityExpression struct {
	ent.Schema
}

// Fields of the PersonalityExpression.
func (PersonalityExpression) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("expression_key").
			NotEmpty().
			MaxLen(64).
			Match(expressionKeyPattern).
			Comment("URL-safe user-defined expression key scoped to a personality"),
		field.String("label").
			Optional().
			Nillable().
			MaxLen(80).
			Comment("Optional label and short guidance for when to use this expression (picker model + chat continuity)"),
	}
}

// Edges of the PersonalityExpression.
func (PersonalityExpression) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("personality", Personality.Type).
			Ref("expressions").
			Unique().
			Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("image", FileAttachment.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("assistant_messages", ChatMessage.Type).
			Ref("generation_expression"),
	}
}

// Indexes of the PersonalityExpression.
func (PersonalityExpression) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("personality").
			Fields("expression_key").
			Unique(),
	}
}

// Mixin of the PersonalityExpression.
func (PersonalityExpression) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
