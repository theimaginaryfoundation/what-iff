package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AuditLog is an append-only operational audit row (admin actions, quota
// mutations, backup import/export, etc.).
type AuditLog struct {
	ent.Schema
}

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Time("occurred_at").
			Default(time.Now).
			Immutable().
			Comment("When the audited action happened"),
		field.String("category").
			NotEmpty().
			MaxLen(64).
			Comment("High-level area, e.g. model, quota, account_backup"),
		field.String("action").
			NotEmpty().
			MaxLen(128).
			Comment("Specific verb, e.g. create, consume, import"),
		field.Text("message").
			Comment("Human-readable description of what happened"),
		field.UUID("actor_user_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Authenticated user that triggered the action when known"),
		field.UUID("subject_user_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Primary user affected by the action when applicable"),
	}
}

func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("occurred_at"),
		index.Fields("category", "occurred_at"),
	}
}

func (AuditLog) Edges() []ent.Edge {
	return nil
}
