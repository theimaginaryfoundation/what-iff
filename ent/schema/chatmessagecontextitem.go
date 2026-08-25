package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ChatMessageContextItem holds a single typed context snippet attached to a ChatMessage.
// These rows replace the former additional_context JSON column and carry the same
// {type, content} shape — e.g. prefetched memories tagged "MEMORY" — but in a
// relational form that is easier to query, extend, and reason about.
type ChatMessageContextItem struct {
	ent.Schema
}

// Fields of the ChatMessageContextItem.
func (ChatMessageContextItem) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("type").
			NotEmpty().
			Comment("Category tag for this context snippet, e.g. MEMORY"),
		field.Text("content").
			NotEmpty().
			Comment("The context snippet body"),
		// Memory provenance. Without these, a rehydrated MEMORY item cannot be tied back to its
		// row or scope, and scope-less refs are dropped when the model context is rebuilt — so the
		// merger and compaction audit would only ever see the current turn's memories.
		field.UUID("memory_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Source memory row for a MEMORY snippet, when known"),
		field.String("scope").
			Optional().
			Comment("Memory scope (User|Chat) for a MEMORY snippet; required to rebuild memory refs"),
	}
}

// Edges of the ChatMessageContextItem.
func (ChatMessageContextItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("chat_message", ChatMessage.Type).
			Ref("context_items").
			Unique().
			Required(),
	}
}

// Indexes of the ChatMessageContextItem.
func (ChatMessageContextItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type"),
		// Used by ChatMessage eager-loading: WHERE chat_message_context_items IN (...)
		index.Edges("chat_message"),
	}
}
