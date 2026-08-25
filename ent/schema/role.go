package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Role holds the schema definition for the Role entity.
type Role struct {
	ent.Schema
}

// Fields of the Role.
func (Role) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			Unique().
			NotEmpty().
			MaxLen(100).
			Comment("The name of the role (e.g., 'admin', 'editor', 'viewer')"),
		field.String("description").
			Optional().
			MaxLen(500).
			Comment("Optional description of the role's purpose and permissions"),
	}
}

// Edges of the Role.
func (Role) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("users", User.Type).
			Ref("roles").
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Mixin of the Role.
func (Role) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
