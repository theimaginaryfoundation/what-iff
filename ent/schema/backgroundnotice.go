package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// BackgroundNotice tells an account that background work on a chat didn't go as
// it should (ADR 0x027 section 6: a failure in background work is shown to the
// account, not only logged). There is at most one per chat and step. The step
// succeeding later deletes it, so fixing the cause clears it.
//
// It states the fact (which step, why, which provider and model); what to tell
// the person and how to fix it is the frontend's.
//
// The edges are one-way, with the foreign keys and cascades on this table.
type BackgroundNotice struct {
	ent.Schema
}

func (BackgroundNotice) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("user_id", uuid.UUID{}).Immutable(),
		field.UUID("chat_id", uuid.UUID{}).Immutable(),
		// What didn't happen (models.BackgroundStep): a checkpoint step, or a
		// utility role passing over a chosen model ("fallback_<role>"). A string so
		// that adding one needs no migration.
		field.String("step").NotEmpty().Immutable(),
		// Why (models.BackgroundNoticeCode).
		field.String("code").NotEmpty(),
		// The provider the step went to, by its display name, when known.
		field.String("provider").Default(""),
		// The model the step ran on, when known.
		field.String("model").Default(""),
		// How many times it has happened since it was last cleared.
		field.Int("count").Default(1),
		// When the account dismissed it. It stays dismissed while the cause (code,
		// provider and model) stays the same.
		field.Time("dismissed_at").Optional().Nillable(),
	}
}

func (BackgroundNotice) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("owner", User.Type).
			Field("user_id").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("chat", Chat.Type).
			Field("chat_id").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BackgroundNotice) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chat_id", "step").Unique(),
	}
}

func (BackgroundNotice) Annotations() []schema.Annotation {
	return []schema.Annotation{
		schema.Comment("Background work on a chat that failed or fell back, until the same step succeeds."),
		entsql.WithComments(true),
	}
}

func (BackgroundNotice) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
