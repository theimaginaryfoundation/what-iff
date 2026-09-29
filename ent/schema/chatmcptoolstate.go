package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ChatMCPToolState stores the loaded MCP tool subset for one chat+connector.
type ChatMCPToolState struct {
	ent.Schema
}

// Fields of the ChatMCPToolState.
func (ChatMCPToolState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("chat_id", uuid.UUID{}),
		field.UUID("mcp_server_id", uuid.UUID{}),
		field.Strings("loaded_tools").
			Optional(),
	}
}

// Indexes of the ChatMCPToolState.
func (ChatMCPToolState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chat_id", "mcp_server_id").Unique(),
		index.Fields("chat_id"),
	}
}

// Edges of the ChatMCPToolState.
func (ChatMCPToolState) Edges() []ent.Edge {
	return nil
}

// Mixin of the ChatMCPToolState.
func (ChatMCPToolState) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
