package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// FileAttachment holds the schema definition for the FileAttachment entity.
type FileAttachment struct {
	ent.Schema
}

// Fields of the FileAttachment.
func (FileAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("file_id").
			Optional(),
		field.String("name").
			NotEmpty(),
		field.String("file_type").
			NotEmpty(),
		field.Text("description").
			Optional().
			Nillable(),
		field.Text("file_content").
			Optional(),
		field.Enum("chunk_status").
			Values("pending", "chunked", "failed").
			Optional().
			Nillable(),
		field.String("s3_key").
			Optional().
			Comment("Canonical S3 object key for the full-resolution file; set at upload time to decouple display name from storage path."),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
	}
}

// Edges of the FileAttachment.
func (FileAttachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("file_attachments").
			Unique().
			Required(),
		edge.From("chat_message", ChatMessage.Type).
			Ref("file_attachments").
			Unique(),
		edge.From("personality", Personality.Type).
			Ref("file_attachments").
			Unique(),
	}
}

// Indexes of the FileAttachment.
func (FileAttachment) Indexes() []ent.Index {
	return []ent.Index{
		// Speed up FK lookups from chat message eager-loading paths.
		index.Edges("chat_message"),
		// Speed up owner-scoped attachment queries.
		index.Edges("owner"),
		// Speed up personality attachment lookups.
		index.Edges("personality"),
	}
}
