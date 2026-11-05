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

// UtilityModelSetting chooses the model for one utility role (background work
// such as memory extraction or chat naming) at one scope (ADR 0x027 section 6):
// the deployment's choice, or an account's. A role with no setting at any scope
// runs on its built-in default.
//
// The edges are one-way, with the foreign keys and cascades on this table:
// deleting an account deletes its settings, and deleting a model (with its
// connection) deletes the settings that chose it, so the role falls back.
type UtilityModelSetting struct {
	ent.Schema
}

func (UtilityModelSetting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// "deployment" or "account" (models.UtilityScope). A string rather than an
		// enum so that adding a scope needs no migration.
		field.String("scope").NotEmpty().Immutable(),
		// The account, for an account setting; empty for the deployment's.
		field.UUID("user_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		// A utility role (models.UtilityRole). A string so that adding a role needs
		// no migration.
		field.String("role").NotEmpty().Immutable(),
		field.UUID("model_id", uuid.UUID{}),
	}
}

func (UtilityModelSetting) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("owner", User.Type).
			Field("user_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("model", Model.Type).
			Field("model_id").
			Unique().
			Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (UtilityModelSetting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope", "user_id", "role").Unique(),
		// The deployment's settings have no user_id, and a unique index treats each
		// NULL as distinct, so they get their own: one per role.
		index.Fields("scope", "role").
			Unique().
			Annotations(entsql.IndexWhere("user_id IS NULL")),
	}
}

func (UtilityModelSetting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		schema.Comment("The model each utility role runs on, per scope. A row with no user_id is the deployment's."),
		entsql.WithComments(true),
	}
}

func (UtilityModelSetting) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
