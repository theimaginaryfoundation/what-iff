package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AgentJob holds the schema definition for the AgentJob entity.
// It represents a scheduled, semi-autonomous agent task that will execute in the future.
type AgentJob struct {
	ent.Schema
}

// Fields of the AgentJob.
func (AgentJob) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("title").
			Optional().
			Nillable().
			Comment("Optional user-facing title for the job"),
		field.Text("prompt").
			NotEmpty().
			Comment("Prompt used to initiate the job run"),
		field.Text("schedule_input").
			NotEmpty().
			Comment("User-provided natural language schedule input"),
		field.Enum("schedule_type").
			Values("cron", "at").
			Comment("Whether this job is scheduled by a cron expression or a one-off run_at time"),
		field.String("schedule").
			Optional().
			Nillable().
			Comment("Quartz cron expression when schedule_type=cron"),
		field.Time("run_at").
			Optional().
			Nillable().
			Comment("Absolute time to run once when schedule_type=at"),
		field.String("timezone").
			Default("UTC").
			NotEmpty().
			Comment("IANA timezone used to interpret recurring schedules (cron). Absolute timestamps (run_at/next_run_at/last_run_at) are stored as UTC instants."),
		field.Enum("status").
			Values("active", "paused", "complete", "failed").
			Default("active").
			Comment("Current job status"),
		field.Time("next_run_at").
			Optional().
			Nillable().
			Comment("Next scheduled run time (best-effort preview)"),
		field.Time("last_run_at").
			Optional().
			Nillable().
			Comment("Most recent run time"),
		field.String("last_error").
			Optional().
			Nillable().
			Comment("Last error encountered during execution"),
		field.Int("run_count").
			Default(0).
			NonNegative().
			Comment("Total number of times this job has executed"),
		field.UUID("personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Optional personality override applied for this job run"),
		field.UUID("model_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Optional model override applied for this job run"),
	}
}

// Edges of the AgentJob.
func (AgentJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("agent_jobs").
			Unique().
			Required(),
		edge.From("chat", Chat.Type).
			Ref("agent_jobs").
			Unique(),
		edge.To("rituals", Ritual.Type),
	}
}

// Indexes of the AgentJob.
func (AgentJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("next_run_at"),
		index.Edges("owner").Fields("status"),
	}
}

// Mixin of the AgentJob.
func (AgentJob) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
