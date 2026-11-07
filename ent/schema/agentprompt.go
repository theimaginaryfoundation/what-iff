package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// AgentPrompt is a question a running turn put to the person and is waiting on (ADR 0x042): the
// agent's ask_user call or an MCP server's elicitation. The turn's worker writes it and polls it;
// the answer arrives through the API, possibly on another instance, so the row is how the two
// meet. Rows are kept after the turn as the record of what was asked.
type AgentPrompt struct {
	ent.Schema
}

// Fields of the AgentPrompt.
func (AgentPrompt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("user_id", uuid.UUID{}).
			Immutable().
			Comment("The account the turn runs for; only it may answer"),
		field.UUID("chat_id", uuid.UUID{}).
			Immutable(),
		field.UUID("job_id", uuid.UUID{}).
			Immutable().
			Comment("The turn's job (a chat_message job)"),
		field.String("source").
			Immutable().
			Comment("Who asks: agent (ask_user) or mcp_elicitation"),
		field.String("tool_call_id").
			Optional().
			Immutable().
			Comment("The provider tool-call id the prompt belongs to"),
		field.JSON("payload", modeltypes.AgentPromptPayload{}).
			Comment("What the card shows"),
		field.Enum("status").
			Values("pending", "answered", "expired", "cancelled").
			Default("pending"),
		field.JSON("response", &modeltypes.AgentPromptResponse{}).
			Optional().
			Comment("What the person sent; null until answered"),
		field.Time("expires_at").
			Comment("When the turn stops waiting"),
		field.Time("answered_at").
			Optional().
			Nillable(),
	}
}

// Indexes of the AgentPrompt.
func (AgentPrompt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("job_id"),
		index.Fields("chat_id", "status"),
	}
}

// Edges of the AgentPrompt.
func (AgentPrompt) Edges() []ent.Edge {
	return nil
}

// Mixin of the AgentPrompt.
func (AgentPrompt) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
