package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// WorkspaceFile is one agent-writable text file: the agent's notebook (root "agent", scoped to a
// personality) or a conversation's working files (root "chat", scoped to a chat). Content lives
// in object storage, one immutable object per revision (WorkspaceFileRevision), so every write is
// auditable and reversible and a stale write can be refused instead of overwriting newer content.
type WorkspaceFile struct {
	ent.Schema
}

// Fields of the WorkspaceFile.
func (WorkspaceFile) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		// root is the namespace: "agent" = a personality's notebook, shared by that personality's
		// conversations; "chat" = one conversation's working files.
		field.Enum("root").
			Values("agent", "chat").
			Immutable(),
		// root_ref is the personality ID (root agent) or chat ID (root chat) the file belongs to.
		field.UUID("root_ref", uuid.UUID{}).
			Immutable(),
		// path is the normalized path inside the root, e.g. "notes/journal.md".
		field.String("path").
			NotEmpty().
			MaxLen(256),
		field.String("content_type").
			NotEmpty(),
		field.Int("current_revision").
			Default(0),
		field.Int64("size").
			Default(0),
		field.String("sha256").
			Default(""),
		// storage_key is the current revision's object (denormalized from its revision row so a
		// read is one query). Empty while the file is deleted.
		field.String("storage_key").
			Default(""),
		// state "deleted" keeps the row and its revisions so a delete can be undone; writing the
		// same path again revives it with a new revision.
		field.Enum("state").
			Values("live", "deleted").
			Default("live"),
		// author_class of the latest revision: who last changed it (the trust class).
		field.Enum("author_class").
			Values("user", "agent", "system").
			Default("agent"),
		field.Time("last_read_at").
			Optional().
			Nillable(),
		field.Int("read_count").
			Default(0),
	}
}

// Edges of the WorkspaceFile.
func (WorkspaceFile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("workspace_files").
			Unique().
			Required(),
		edge.To("revisions", WorkspaceFileRevision.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the WorkspaceFile.
func (WorkspaceFile) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner").Fields("root", "root_ref", "path").Unique(),
		index.Edges("owner").Fields("root", "root_ref", "state"),
	}
}

// Mixin of the WorkspaceFile.
func (WorkspaceFile) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
