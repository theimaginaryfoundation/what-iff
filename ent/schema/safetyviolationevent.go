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

// SafetyViolationEvent holds the schema definition for provider safety rejection events.
type SafetyViolationEvent struct {
	ent.Schema
}

// Fields of the SafetyViolationEvent.
func (SafetyViolationEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Time("occurred_at").
			Default(time.Now).
			Immutable().
			Comment("Timestamp when the provider safety rejection occurred"),
		field.Enum("provider").
			Values("openai", "anthropic", "google", "zai", "mistral", "deepseek", "qwen", "xiaomi", "local").
			Comment("Provider that rejected the request"),
		field.String("violation_type").
			Optional().
			Nillable().
			Comment("Best-effort normalized violation category"),
		field.String("provider_code").
			Optional().
			Nillable().
			Comment("Provider-specific rejection code"),
		field.Text("provider_message").
			Comment("Provider rejection message"),
		field.Text("raw_error").
			Comment("Raw error payload/message from provider"),
		field.UUID("chat_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Associated chat if available"),
		field.UUID("chat_message_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Associated triggering chat message if available"),
		field.String("chat_name").
			Default("").
			Comment("Snapshot of chat name for moderation review"),
		field.Text("chat_message_text").
			Default("").
			Comment("Snapshot of triggering message text for moderation review"),
	}
}

// Edges of the SafetyViolationEvent.
func (SafetyViolationEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("safety_violation_events").
			Unique().
			Required(),
		edge.From("chat", Chat.Type).
			Ref("safety_violation_events").
			Unique().
			Field("chat_id").
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("chat_message", ChatMessage.Type).
			Ref("safety_violation_events").
			Unique().
			Field("chat_message_id").
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

// Indexes of the SafetyViolationEvent.
func (SafetyViolationEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("occurred_at"),
		index.Fields("provider"),
		index.Edges("user").Fields("occurred_at"),
		index.Edges("chat").Fields("occurred_at"),
		index.Edges("chat_message").Fields("occurred_at"),
	}
}

// Mixin of the SafetyViolationEvent.
func (SafetyViolationEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
