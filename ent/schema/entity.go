package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Entity is a person, place, project or other thing in the user's world that the agent keeps a
// short card about, so that mentioning it ("John", "the Italy trip") brings the card into context.
// Names and aliases live in EntityAlias, which is what mentions are matched against.
type Entity struct {
	ent.Schema
}

// Fields of the Entity.
func (Entity) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			MaxLen(100),
		// type is free-form ("person", "project", "pet", "npc"): typing is descriptive, never an enum.
		field.String("type").
			Default("").
			MaxLen(40),
		// card is the short note injected when the entity is mentioned.
		field.Text("card").
			Default(""),
		// revision guards card edits: a change must name the revision it was based on.
		field.Int("revision").
			Default(1),
		field.Enum("state").
			Values("active", "archived").
			Default("active"),
		// author_class is who last changed the card (the trust class).
		field.Enum("author_class").
			Values("user", "agent", "system").
			Default("agent"),
		// pinned_personality_id, when set, makes the entity visible to that personality only.
		field.UUID("pinned_personality_id", uuid.UUID{}).
			Optional().
			Nillable(),
		field.Time("card_updated_at").
			Default(time.Now),
	}
}

// Edges of the Entity.
func (Entity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("entities").
			Unique().
			Required(),
		edge.To("aliases", EntityAlias.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the Entity.
func (Entity) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner").Fields("state"),
	}
}

// Mixin of the Entity.
func (Entity) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
