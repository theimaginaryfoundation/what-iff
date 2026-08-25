package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// WebhookToken holds the schema definition for static API tokens used by webhook routes.
type WebhookToken struct {
	ent.Schema
}

// Fields of the WebhookToken.
func (WebhookToken) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			MaxLen(120),
		field.String("token_hash").
			NotEmpty().
			Sensitive(),
		field.Enum("status").
			Values("active", "revoked").
			Default("active"),
		field.Time("last_used_at").
			Optional().
			Nillable(),
	}
}

// Edges of the WebhookToken.
func (WebhookToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("webhook_tokens").
			Unique().
			Required(),
	}
}

// Indexes of the WebhookToken.
func (WebhookToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique(),
		index.Edges("owner").Fields("status"),
	}
}

// Mixin of the WebhookToken.
func (WebhookToken) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
