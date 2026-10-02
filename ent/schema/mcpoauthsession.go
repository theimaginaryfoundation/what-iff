package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// MCPOAuthSession tracks OAuth authorization attempts for connector auth callbacks.
type MCPOAuthSession struct {
	ent.Schema
}

func (MCPOAuthSession) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("state").
			NotEmpty(),
		field.String("code_verifier").
			Optional().
			Sensitive(),
		field.Time("expires_at"),
		field.Time("consumed_at").
			Optional().
			Nillable(),
		field.String("redirect_after").
			Optional(),
	}
}

func (MCPOAuthSession) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("mcp_oauth_sessions").
			Unique().
			Required(),
		edge.From("mcp_server", MCPServer.Type).
			Ref("oauth_sessions").
			Unique().
			Required(),
	}
}

func (MCPOAuthSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("state").Unique(),
		index.Fields("expires_at"),
		index.Fields("consumed_at"),
		index.Edges("owner"),
		index.Edges("mcp_server"),
	}
}

func (MCPOAuthSession) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
