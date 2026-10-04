package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// WorkspaceFileRevision is one immutable version of a WorkspaceFile. It is both the audit trail
// (who changed what, when, from which conversation, on top of which revision) and the undo stack.
// Its content is the object at storage_key; delete revisions have no object.
type WorkspaceFileRevision struct {
	ent.Schema
}

// Fields of the WorkspaceFileRevision.
func (WorkspaceFileRevision) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Int("revision").
			Immutable(),
		field.Enum("op").
			Values("create", "write", "append", "edit", "delete", "revert").
			Immutable(),
		// base_revision is the revision the writer last saw; 0 for a create or an append.
		field.Int("base_revision").
			Default(0).
			Immutable(),
		field.String("storage_key").
			Default("").
			Immutable(),
		field.Int64("size").
			Default(0).
			Immutable(),
		field.String("sha256").
			Default("").
			Immutable(),
		field.Enum("author_class").
			Values("user", "agent", "system").
			Immutable(),
		// chat_id records which conversation made the change (provenance), when there was one.
		field.UUID("chat_id", uuid.UUID{}).
			Optional().
			Nillable().
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the WorkspaceFileRevision.
func (WorkspaceFileRevision) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("file", WorkspaceFile.Type).
			Ref("revisions").
			Unique().
			Required(),
	}
}

// Indexes of the WorkspaceFileRevision.
func (WorkspaceFileRevision) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("file").Fields("revision").Unique(),
	}
}
