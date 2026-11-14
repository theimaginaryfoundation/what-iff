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

// UtilityModelSetting is the choice for one utility role (background work such as
// memory extraction or chat naming) at one scope (ADR 0x027 section 6): the
// deployment's, or an account's. A setting chooses a model or switches the role
// off; a role with no setting at any scope is not set, and its work doesn't run.
//
// The edges are one-way, with the foreign keys and cascades on this table:
// deleting an account deletes its settings, and deleting a model row (with its
// connection) deletes the settings that chose it, leaving the role not set
// rather than off. A model turned off or retired keeps its row, and its setting;
// the agent passes over a choice it can't use to the next scope's.
type UtilityModelSetting struct {
	ent.Schema
}

func (UtilityModelSetting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// "deployment" or "account" (models.UtilityScope). A string rather than an
		// enum, so adding a scope changes no column type; the CHECK constraint in
		// Annotations names each scope, though, and must gain the new one.
		field.String("scope").NotEmpty().Immutable(),
		// The account, for an account setting; empty for the deployment's.
		field.UUID("user_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		// A utility role (models.UtilityRole). A string so that adding a role needs
		// no migration.
		field.String("role").NotEmpty().Immutable(),
		// The chosen model; NULL switches the role off.
		field.UUID("model_id", uuid.UUID{}).Optional().Nillable(),
		// The reasoning effort the role's calls ask for (models.ReasoningEffort); NULL leaves
		// the model's own default. A string, like role, validated by the datastore.
		field.String("reasoning_effort").Optional().Nillable(),
		// The service tier the role's calls ask for (models.UtilityServiceTier); NULL leaves the
		// API's default.
		field.String("service_tier").Optional().Nillable(),
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
		schema.Comment("The model each utility role runs on, per scope; no model_id switches the role off. A row with no user_id is the deployment's."),
		entsql.WithComments(true),
		// The datastore checks this before writing; the database holds to it too, so no
		// write path can leave a deployment setting with an owner or an account's without one.
		entsql.Checks(map[string]string{
			"utility_model_setting_scope_owner": "((scope = 'deployment' AND user_id IS NULL) OR (scope = 'account' AND user_id IS NOT NULL))",
			// Off has no model, so nothing to ask it for.
			"utility_model_setting_off_is_bare": "(model_id IS NOT NULL OR (reasoning_effort IS NULL AND service_tier IS NULL))",
		}),
	}
}

func (UtilityModelSetting) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
