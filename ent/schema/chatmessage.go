package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// ChatMessage holds the schema definition for the ChatMessage entity.
// It represents a single message within a chat session from either the user or the assistant.
type ChatMessage struct {
	ent.Schema
}

// Fields of the ChatMessage.
func (ChatMessage) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Text("message").
			Comment("The actual message content; empty string is allowed (e.g. assistant turns that only emitted tool calls)."),
		field.Enum("origin").
			Values("User", "Assistant").
			Comment("Whether the message originates from the user or the assistant"),
		field.Enum("read_status").
			Values("read", "unread").
			Default("read").
			Comment("Read status for assistant-origin messages"),
		field.String("response_id").
			Optional().
			Comment("OpenAI response ID for assistant messages to support traceability"),
		field.Time("sent_at").
			Default(time.Now).
			Immutable().
			Comment("When the message was sent"),
		field.Int64("tokens").
			Optional().
			Comment("Number of tokens consumed for the message"),
		field.String("generation_model").
			Optional().
			Comment("Model used to generate this message"),
		field.String("generation_personality").
			Optional().
			Comment("Personality active when this message was generated"),
		field.Text("generation_expression_reasoning").
			Optional().
			Nillable().
			Comment("Short classifier rationale for the chosen generation_expression portrait (assistant messages)"),
		field.Text("model_reasoning").
			Optional().
			Nillable().
			Comment("Provider-reported reasoning/thinking text for an assistant turn (e.g. GLM thinking blocks, MiMo reasoning_content), concatenated across the turn's tool rounds. Display-only: never replayed into model context."),
		field.JSON("agent_run", &modeltypes.AgentRun{}).
			Optional().
			Comment("Structured record of an Advanced-loop turn (ADR 0x026): its steps, plan and end reason. Display and continuation only; NULL on Standard turns."),
		field.JSON("job_run", &modeltypes.JobRun{}).
			Optional().
			Comment("The agent job a scheduled run's reply was written for, with the prompt it ran (ADR 0x040). Display only: never replayed into model context. NULL on every other message."),
		field.Text("last_error_message").
			Optional().
			Nillable().
			Comment("User-visible generation failure for this user message; cleared on successful delivery"),
		field.String("last_error_code").
			Optional().
			Nillable().
			Comment("Stable code for last_error_message when it has one (models error codes); set and cleared with it"),
		field.Time("checkpoint_completed_at").
			Optional().
			Nillable().
			Comment("When scratchpad + memory + summary checkpoint finished for the turn ending with this assistant message"),
		field.JSON("context_breakdown", &modeltypes.ContextBreakdown{}).
			Optional().
			Comment("jsonb snapshot (modeltypes.ContextBreakdown) of the model-context segment/token composition captured at generation time for this assistant message; used by the Context X-ray UI. jsonb (not text) keeps DB-side JSON queries open; a version field inside guards shape evolution."),
		field.JSON("comment_refs", []modeltypes.CommentRef{}).
			Optional().
			Comment("Document comments the message is about (ADR 0x046): on a user message the comment that sent it, on an assistant message the comments it made or replied to. Shown as links to the thread."),
		// Where the reply's final round starts in message (ADR 0x052 §6), in bytes: text
		// before it was written before a tool call, and is not read aloud. Zero for a
		// reply with no tool rounds, and for every message before speech.
		field.Int("final_round_offset").
			Default(0).
			NonNegative(),
		field.Bool("bookmarked").
			Default(false).
			Comment("User-flagged bookmark for navigating long threads; either origin can be bookmarked."),
		field.Bool("exclude_from_memory").
			Optional().
			Nillable().
			Comment("Set to true on both messages of a turn sent with memory saving off (models.TurnOptions.MemorySave): a checkpoint whose conversation includes a flagged message extracts no memories from it. NULL on every ordinary message. A column, not turn state in memory, because extraction runs at a later checkpoint, possibly in another process."),
		field.Time("superseded_at").
			Optional().
			Nillable().
			Comment("When the message was taken back (agent.SupersedeTurn, or the reply of a regenerated turn). A superseded message is kept as a past version but left out of the model's context, history listings and later memory extraction. NULL on every live message."),
		field.UUID("superseded_by", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("The reply that replaced this one when its turn was regenerated, set once the new reply is saved. NULL when nothing replaced it (a turn taken back outright) and on every live message."),
	}
}

// Edges of the ChatMessage.
func (ChatMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("chat", Chat.Type).
			Ref("messages").
			Unique().
			Required(),
		edge.To("file_attachments", FileAttachment.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("rituals", Ritual.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("tool_calls", ToolCall.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("context_items", ChatMessageContextItem.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("safety_violation_events", SafetyViolationEvent.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
		// Optional: records which mood was active when this assistant message was generated.
		edge.To("generation_mood", Mood.Type).
			Unique(),
		// Optional: personality expression portrait chosen for this assistant message.
		edge.To("generation_expression", PersonalityExpression.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

// Indexes of the ChatMessage.
func (ChatMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("origin"),
		index.Fields("origin", "read_status"),
		index.Fields("response_id"),
		// ent orders an index's plain fields before its edge columns, so every composite
		// below ends with the chat FK and none can seek by chat alone. This one is what
		// per-chat counts and listings (WHERE chat_messages = ?) use.
		index.Edges("chat"),
		index.Edges("chat").Fields("sent_at"),
		index.Edges("chat").Fields("origin", "read_status"),
		// Serves the per-chat bookmarks listing (WHERE chat=? AND bookmarked=true).
		index.Edges("chat").Fields("bookmarked"),
	}
}
