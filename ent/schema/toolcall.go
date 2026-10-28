package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ToolCall holds the schema definition for the ToolCall entity.
type ToolCall struct {
	ent.Schema
}

// Fields of the ToolCall.
func (ToolCall) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("tool_name").
			NotEmpty().
			Comment("The name of the tool"),
		field.Text("tool_input").
			Optional().
			Comment("The input arguments of the tool call"),
		field.Text("tool_output").
			Optional().
			Comment("The output from the tool call execution"),
		field.Text("tool_error").
			Optional().
			Comment("The error from the tool call execution"),
		field.Int("step").
			Optional().
			Default(0).
			Comment("Advanced-loop step (ADR 0x026) the call ran in, 1-based; 0 for Standard turns."),
	}
}

// Edges of the ToolCall.
func (ToolCall) Edges() []ent.Edge {
	return []ent.Edge{
		// chat_message is the message the call belongs to. It is unset only while an Advanced
		// run is in progress: the call is then linked to the run's job instead (draft_job), and
		// moves onto the reply when the run ends (ADR 0x026).
		edge.From("chat_message", ChatMessage.Type).
			Ref("tool_calls").
			Unique(),
		edge.From("draft_job", Job.Type).
			Ref("draft_tool_calls").
			Unique(),
	}
}

// Indexes of the ToolCall.
func (ToolCall) Indexes() []ent.Index {
	return []ent.Index{
		// Used by ChatMessage eager-loading: WHERE chat_message_tool_calls IN (...)
		index.Edges("chat_message"),
		index.Edges("draft_job"),
	}
}

// Mixin of the ToolCall.
func (ToolCall) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
