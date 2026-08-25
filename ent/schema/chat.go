package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// Chat holds the schema definition for the Chat entity.
// It represents a named chat session that allows users to maintain specific contexts for particular tasks.
type Chat struct {
	ent.Schema
}

// Fields of the Chat.
func (Chat) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			MaxLen(200).
			Comment("Name of the chat session, auto-generated from first message but user can edit"),
		field.String("response_id").
			Optional().
			Comment("Most recent OpenAI response ID for assistant messages to support traceability"),
		field.Text("checkpoint_summary").
			Optional().
			Comment("Compact conversation summary used to seed a new Responses thread after checkpointing"),
		field.Int("checkpoint_user_message_count").
			Default(0).
			Comment("Total number of user messages processed at the last checkpoint; used to compute messages-since-checkpoint"),
		field.Time("last_message_time").
			Optional().
			Nillable().
			Comment("Timestamp of the most recent message in this chat, used for sorting chats by activity"),
		field.Time("last_checkpoint_at").
			Optional().
			Nillable().
			Comment("Timestamp of the most recent checkpoint rollover; used to scope history loads to the current checkpoint period"),
		field.Strings("disabled_tools").
			Optional().
			Comment("Tool names the user has explicitly disabled for this chat; nil/empty means all default tools are enabled"),
		field.Strings("tags").
			Optional().
			Validate(modeltypes.ValidateChatTags).
			Comment("User-defined labels for this chat thread; max 10 tags with max 10 characters per tag"),
		field.Bool("is_favorite").
			Default(false).
			Comment("Whether this thread is marked as a user favorite"),
		field.Bool("is_auto_mood").
			Default(true).
			Comment("Whether mood selection is automatic for this chat; true keeps mood tools enabled while active_mood stores the current effective mood"),
		field.Bool("archived").
			Default(false).
			Comment("When true, thread is hidden from default lists; list with archived=true to browse the archive"),
		field.String("source").
			Optional().
			Comment("Origin of chat, e.g. 'openai'/'anthropic' for imported conversation exports"),
		field.String("import_hash").
			Optional().
			Comment("Per-conversation dedup hash (sha256 of conversationID from the export); set on imported chats"),
		field.String("rehydration_state").
			Optional().
			Comment("Lazy-summarization lifecycle for imported threads: ''/'pending'/'processing'/'ready'/'failed'. Empty means no rehydration needed (e.g. native threads)"),
	}
}

// Edges of the Chat.
func (Chat) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("chats").
			Unique().
			Required(),
		edge.To("messages", ChatMessage.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("memories", Memory.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("agent_jobs", AgentJob.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("safety_violation_events", SafetyViolationEvent.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("mcp_servers", MCPServer.Type),
		edge.To("model", Model.Type).
			Unique(),
		edge.To("personality", Personality.Type).
			Unique(),
		// Optional: the mood currently active for this conversation.
		edge.To("active_mood", Mood.Type).
			Unique(),
	}
}

// Indexes of the Chat.
func (Chat) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name"),
		index.Edges("owner").Fields("last_message_time"),
		index.Edges("owner").Fields("import_hash").Unique(),
	}
}

// Mixin of the Chat.
func (Chat) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
