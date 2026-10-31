package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Ritual holds the schema definition for the Ritual entity.
type Ritual struct {
	ent.Schema
}

// Fields of the Ritual.
func (Ritual) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			Comment("The name of the ritual"),
		field.Text("description").
			NotEmpty().
			Comment("The description of the ritual"),
		field.Text("content").
			NotEmpty().
			Comment("The content of the ritual"),
		field.String("hotkeys").
			Comment("The keyboard shortcut hotkeys of the ritual"),
		// Agent Skills fields (ADR 0x035 §1). SKILL.md is rendered from these columns, never stored.
		field.String("slug").
			Default("").
			MaxLen(64).
			Comment("The skill's name in the Agent Skills sense: 1-64 chars of [a-z0-9-], unique per owner. Empty only on rows created before the column existed, until the startup backfill derives one."),
		field.String("license").
			Default("").
			MaxLen(500).
			Comment("SKILL.md license: free text"),
		field.String("compatibility").
			Default("").
			MaxLen(500).
			Comment("SKILL.md compatibility: what the skill needs from the environment, shown when its scripts cannot run here"),
		field.JSON("metadata", map[string]string{}).
			Optional().
			Comment("SKILL.md metadata: string-to-string client extensions; our own keys are prefixed whatiff-"),
		field.Strings("allowed_tools").
			Optional().
			Comment("SKILL.md allowed-tools: tools loaded with the skill (never a permission grant)"),
		field.Bool("disable_model_invocation").
			Default(false).
			Comment("When true the skill is left out of the model's skills index and can only be invoked by the user, a mode or a job"),
		field.Bool("user_invocable").
			Default(true).
			Comment("When false the skill is hidden from the /skill picker and hotkeys; the model may still load it"),
	}
}

// Edges of the Ritual.
func (Ritual) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("rituals").
			Unique().
			Required(),
		edge.To("personality", Personality.Type).
			Unique(),
		edge.From("chat_messages", ChatMessage.Type).
			Ref("rituals"),
		edge.To("mcp_servers", MCPServer.Type),
		// Inverse of Mood.rituals and AgentJob.rituals. Declaring the back-reference makes both
		// edges many-to-many (join tables mood_rituals and agent_job_rituals) instead of the
		// one-to-many FK columns ent produced without it, which let a skill sit on only one mode
		// or job at a time (ADR 0x035 §11).
		edge.From("moods", Mood.Type).
			Ref("rituals"),
		edge.From("agent_jobs", AgentJob.Type).
			Ref("rituals"),
		// The skill bundle's files, as file_attachments rows with origin = skill (ADR 0x035 §3).
		// Deleting the skill deletes its files' rows; the handler releases their objects first.
		edge.To("files", FileAttachment.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the Ritual.
func (Ritual) Indexes() []ent.Index {
	return []ent.Index{
		// One slug per owner. Partial so legacy rows awaiting the slug backfill can share "".
		index.Fields("slug").
			Edges("owner").
			Unique().
			Annotations(entsql.IndexWhere("slug <> ''")),
	}
}

// Mixin of the Ritual.
func (Ritual) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
