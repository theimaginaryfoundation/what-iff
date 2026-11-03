package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Marker records that a one-time step at boot has run, so it never runs again,
// whatever the data looks like afterwards. The first is the conversion of a
// self-hosted install's seeded catalog (ADR 0x027).
type Marker struct {
	ent.Schema
}

// Fields of the Marker.
func (Marker) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty().
			MaxLen(100).
			Immutable().
			Comment("Which step ran, e.g. 'seeded_catalog_converted'"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}
