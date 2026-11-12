package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// FileEditGrant lets the agent of a chat change the content of one of the user's files (ADR
// 0x046). Agents change only the files they created (ADR 0x034); mentioning the agent in a
// comment on a file while in a chat grants that chat's agent the file as well, so it can make the
// change the comment asks for. The file is the original row of a file and its chat copies.
type FileEditGrant struct {
	ent.Schema
}

// Fields of the FileEditGrant.
func (FileEditGrant) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("file_attachment_id", uuid.UUID{}).
			Immutable(),
		field.UUID("chat_id", uuid.UUID{}).
			Immutable(),
		// The chat's personality when the grant was made: switching the chat to another
		// personality does not pass the grant on (nil for a chat with none).
		field.UUID("personality_id", uuid.UUID{}).
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the FileEditGrant.
func (FileEditGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("file_attachment", FileAttachment.Type).
			Ref("edit_grants").
			Field("file_attachment_id").
			Unique().
			Required().
			Immutable(),
		edge.From("chat", Chat.Type).
			Ref("file_edit_grants").
			Field("chat_id").
			Unique().
			Required().
			Immutable(),
	}
}

// Indexes of the FileEditGrant.
func (FileEditGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("file_attachment_id", "chat_id").Unique(),
	}
}
