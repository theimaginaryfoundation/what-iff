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
		field.String("folder").
			Default("").
			MaxLen(255).
			Comment("Gallery folder path, e.g. \"charts/oura\"; \"\" is the top level. A label only: the S3 key is unaffected by moves."),
		field.Bool("context_only").
			Default(false).
			Comment("A reference row shown with an assistant reply for an existing file the agent fetched to look at (find_context). Not a new output: kept out of the gallery and not relayed."),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		// Who wrote the file (ADR 0x034). An agent may change only "agent" files its personality
		// wrote, or, with no personality, the files written in its conversation; every other file,
		// including all of the user's uploads, is read-only to agents. A string enum (varchar in
		// Postgres), so a new origin is a new value, not a migration.
		field.Enum("origin").
			Values("user", "agent").
			Default("user").
			Comment("Who wrote the file: \"user\" (uploads, imports, reply outputs) or \"agent\" (an agent's file tools). Only agent files can be changed by an agent."),
		field.UUID("created_by_personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Personality whose agent created the file; the owner for agent edits. Null with no personality."),
		field.UUID("created_by_chat_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Conversation the agent created the file in; the owner for agent edits when there is no personality."),
		field.Time("updated_at").
			Optional().
			Nillable().
			Comment("When an agent last changed the content. Null for files never changed after upload."),
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
		// Backs the gallery's reference-copy exclusion, a correlated
		// (owner, s3_key) lookup per listed row.
		index.Fields("s3_key").Edges("owner"),
		// Backs gallery folder navigation and the agent's folder-prefix listing.
		index.Fields("folder").Edges("owner"),
	}
}
