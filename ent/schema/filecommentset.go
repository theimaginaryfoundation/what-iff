package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// FileCommentSet holds the comment threads of a text or code file (ADR 0x045). Those formats have
// no place for comments, so they live here, beside the file: one set per file, with the text the
// threads' anchors were placed against, so a change to the file made anywhere (an agent's file
// tools, another save) can carry the anchors across when the comments are next read.
type FileCommentSet struct {
	ent.Schema
}

// Fields of the FileCommentSet.
func (FileCommentSet) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("file_attachment_id", uuid.UUID{}).
			Immutable().
			Unique(),
		field.Text("threads").
			Default("[]").
			Comment("The threads as JSON (models.FileCommentThread); anchors are UTF-16 ranges of the baseline text."),
		field.Text("baseline_text").
			Default("").
			Comment("The file's text the anchors are placed against; a read after the file changed carries them to its new text."),
		field.Int("baseline_version").
			Default(1).
			Comment("The file's content_version the baseline is."),
		field.Int("revision").
			Default(1).
			Positive().
			Comment("Bumped by every write; a write sends the revision it read, and a mismatch is refused."),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the FileCommentSet.
func (FileCommentSet) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("file_attachment", FileAttachment.Type).
			Ref("comment_set").
			Field("file_attachment_id").
			Unique().
			Required().
			Immutable(),
	}
}
