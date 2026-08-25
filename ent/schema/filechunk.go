package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// FileChunk holds the schema definition for the FileChunk entity.
type FileChunk struct {
	ent.Schema
}

// Fields of the FileChunk.
func (FileChunk) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Text("content").
			NotEmpty(),
		field.Other("embedding", pgvector.Vector{}).
			SchemaType(map[string]string{
				dialect.Postgres: "vector(1536)",
			}),
		field.Int("sequence"),
		field.JSON("metadata", map[string]string{}).
			Optional(),
	}
}

// Edges of the FileChunk.
func (FileChunk) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("file_attachment", FileAttachment.Type).
			Unique().
			Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the FileChunk.
func (FileChunk) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("embedding").
			Annotations(
				entsql.IndexType("hnsw"),
				entsql.OpClass("vector_l2_ops"),
			),
	}
}
