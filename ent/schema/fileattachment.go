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
			Values("user", "agent", "skill").
			Default("user").
			Comment("Who wrote the file: \"user\" (uploads, imports, reply outputs), \"agent\" (an agent's file tools) or \"skill\" (a skill bundle's file, ADR 0x035). Only agent files can be changed by an agent."),
		field.UUID("skill_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("The skill (ritual) whose bundle this file belongs to, for origin = skill; the row goes with the skill."),
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
			Comment("When the content was last changed, by an agent's file tools or by the user saving in the file workspace (ADR 0x043). Null for files never changed after upload."),
		field.Int64("size_bytes").
			Optional().
			Nillable().
			NonNegative().
			Comment("Bytes in the stored object. Set for skill bundle files (ADR 0x035 §3) and by every content write (ADR 0x043); null on rows uploaded before that and never changed."),
		// The file workspace's optimistic concurrency token (ADR 0x043 §3): every content write
		// bumps it, a save sends the version it loaded as If-Match, and a mismatch is a 412.
		field.Int("content_version").
			Default(1).
			Positive().
			Comment("Bumped by every content write. A save sends the version it loaded back as If-Match; a mismatch is refused."),
		field.String("content_sha256").
			Optional().
			Comment("Hex SHA-256 of the stored object, set by every content write (ADR 0x043); empty on rows uploaded before that and never changed. Lets a container compare its copy without reading the object."),
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
		// A skill bundle's files (ADR 0x035 §3): deleting the skill deletes its files' rows (the
		// cascade is declared on Ritual.files); the handler releases their objects first.
		edge.From("skill", Ritual.Type).
			Ref("files").
			Unique().
			Field("skill_id"),
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
		// Backs a skill's file listing and its cascade, and keeps one file per path in a bundle.
		// Rows outside a bundle have a null skill_id and never conflict.
		index.Fields("skill_id", "folder", "name").Unique(),
	}
}
