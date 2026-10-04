package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// EntityAlias is one name an entity is known by (its canonical name is always one of them). Mentions
// in messages are matched against alias_norm. owner_id and scope_key are denormalized from the
// entity so the unique index can guarantee a name means one entity within a scope.
type EntityAlias struct {
	ent.Schema
}

// Fields of the EntityAlias.
func (EntityAlias) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("owner_id", uuid.UUID{}),
		// scope_key is the entity's pinned personality ID, or the nil UUID for an entity every
		// personality can see. (A non-null key keeps the unique index effective on Postgres.)
		field.UUID("scope_key", uuid.UUID{}),
		field.String("alias").
			NotEmpty().
			MaxLen(100),
		// alias_norm is the lowercased, punctuation-collapsed form used for matching.
		field.String("alias_norm").
			NotEmpty().
			MaxLen(100),
	}
}

// Edges of the EntityAlias.
func (EntityAlias) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("entity", Entity.Type).
			Ref("aliases").
			Unique().
			Required(),
	}
}

// Indexes of the EntityAlias.
func (EntityAlias) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id", "scope_key", "alias_norm").Unique(),
		index.Fields("owner_id"),
	}
}
