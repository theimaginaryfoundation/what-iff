package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Weave is a named set of sandboxed threads that may read each other: each member's sandbox
// boundary widens from "this thread" to "this thread and its weave peers" for reads (Chat-scoped
// memories, checkpoint summaries, messages), while every write stays in the thread that makes it.
// Members are the chats whose weave_id is this weave; a chat is in at most one weave. See
// docs/adr/0x030-thread-weaves.md.
type Weave struct {
	ent.Schema
}

func (Weave) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("name").NotEmpty().MaxLen(100),
	}
}

func (Weave) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("weaves").Unique().Required(),
	}
}

func (Weave) Indexes() []ent.Index {
	return []ent.Index{index.Edges("owner")}
}

func (Weave) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
