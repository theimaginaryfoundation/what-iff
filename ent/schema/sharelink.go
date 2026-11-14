package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// ShareLink is a public link to one of the user's files (ADR 0x051): anyone with the link (and
// the passcode, when it needs one) sees the file, read-only, on the share site. An HTML file in a
// gallery folder is shared as a web app: the page and its folder's files. The file is the
// original row of a file and its chat copies; deleting it deletes the share.
type ShareLink struct {
	ent.Schema
}

// Fields of the ShareLink.
func (ShareLink) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		// The link's key: 128 random bits, base64url. It names neither the user nor the file.
		field.String("key").
			NotEmpty().
			Unique().
			Immutable().
			Sensitive(),
		field.UUID("file_attachment_id", uuid.UUID{}).
			Unique().
			Immutable(),
		// The passcode, encrypted with the token key; empty when the link needs none.
		field.String("passcode_ciphertext").
			Default("").
			Sensitive(),
		// Counts the passcodes the link has had: a visitor's unlock is good for one generation, so
		// a new passcode (or turning one on) signs everyone out.
		field.Int("passcode_generation").
			Default(1),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the ShareLink.
func (ShareLink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("file_attachment", FileAttachment.Type).
			Ref("share_link").
			Field("file_attachment_id").
			Unique().
			Required().
			Immutable(),
	}
}
