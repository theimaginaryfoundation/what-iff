package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// User holds the schema definition for the User entity.
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return append([]ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("username").
			Unique().
			MinLen(3).
			MaxLen(50),
		field.String("email").
			Unique().
			MaxLen(255),
		field.String("password_hash").
			Sensitive().
			NotEmpty(),
		field.String("first_name").
			Optional().
			MaxLen(50),
		field.String("last_name").
			Optional().
			MaxLen(50),
		field.String("timezone").
			Default("America/New_York").
			MaxLen(64).
			Comment("IANA timezone for scheduling (e.g. America/New_York)"),
		field.Enum("status").
			Values("active", "inactive").
			Default("active").
			Comment("User account status (active, inactive)"),
		field.Bool("enable_experimental_models").
			Default(false).
			Comment("When true, user can see and use google/zai (Gemini/GLM) models"),
		field.Time("last_login").
			Optional().
			Nillable(),
		field.Time("last_seen").
			Optional().
			Nillable(),
		field.Time("terms_accepted_at").
			Optional().
			Nillable().
			Comment("Timestamp when user accepted Terms of Service"),
		field.String("refresh_token_id").
			Optional().
			Sensitive(),
	}, userPrivateFields()...)
}

// Edges of the User.
func (User) Edges() []ent.Edge {
	return append([]ent.Edge{
		edge.To("roles", Role.Type).
			Comment("User roles for authorization (many-to-many)"),
		edge.To("jobs", Job.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("agent_jobs", AgentJob.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("chats", Chat.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("memories", Memory.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("preferences", UserPreference.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("personalities", Personality.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("file_attachments", FileAttachment.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("rituals", Ritual.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("moods", Mood.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("mcp_servers", MCPServer.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("webhook_tokens", WebhookToken.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("system_ritual_bindings", SystemRitualBinding.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("personality_gen_flows", PersonalityGenFlow.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("safety_violation_events", SafetyViolationEvent.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}, userPrivateEdges()...)
}

// Mixin of the User.
func (User) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
