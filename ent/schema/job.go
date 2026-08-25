package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Job holds the schema definition for the Job entity.
// It represents an asynchronous background job like processing a chat message.
type Job struct {
	ent.Schema
}

// Fields of the Job.
func (Job) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("job_type").
			NotEmpty().
			Comment("Type of job being processed (e.g., chat_message)"),
		field.String("reference").
			NotEmpty().
			Comment("External reference or identifier for the job"),
		field.Enum("status").
			Values("pending", "processing", "inference_complete", "expression_complete", "compaction_complete", "complete", "cancelled", "failed").
			Default("pending").
			Comment("Current status of the job"),
		field.String("error").
			Optional().
			Comment("Error message if the job failed"),
		field.UUID("result_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("UUID of the result entity after successful completion"),
		field.JSON("draft_deltas", []string{}).
			Optional().
			Comment("Incremental assistant text chunks persisted while inference is still running"),
		field.Text("progress").
			Optional().
			Comment("Optional JSON-encoded progress payload for long-running jobs (e.g. chat import imported/skipped/total counts)"),
	}
}

// Edges of the Job.
func (Job) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("jobs").
			Unique().
			Required(),
	}
}

// Indexes of the Job.
func (Job) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("job_type"),
		index.Fields("reference"),
	}
}

// Mixin of the Job.
func (Job) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
