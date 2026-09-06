package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// MCPServer holds the schema definition for remote MCP servers configured by a user.
type MCPServer struct {
	ent.Schema
}

// Fields of the MCPServer.
func (MCPServer) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			MaxLen(120),
		field.String("description").
			NotEmpty().
			MaxLen(500),
		field.String("server_url").
			NotEmpty().
			MaxLen(2048),
		field.String("auth_token").
			Optional().
			Sensitive(),
		field.String("status").
			Default("active"),
		field.String("status_reason").
			Optional().
			Default(""),
		field.Time("last_checked_at").
			Optional().
			Nillable(),
		field.Time("last_healthy_at").
			Optional().
			Nillable(),
		field.Int("tool_count").
			Default(0),
		field.Bool("default_enabled").
			Default(false),
	}
}

// Edges of the MCPServer.
func (MCPServer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("mcp_servers").
			Unique().
			Required(),
		edge.From("chats", Chat.Type).
			Ref("mcp_servers"),
		edge.From("rituals", Ritual.Type).
			Ref("mcp_servers"),
	}
}

// Indexes of the MCPServer.
func (MCPServer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name"),
		index.Edges("owner"),
	}
}

// Mixin of the MCPServer.
func (MCPServer) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
