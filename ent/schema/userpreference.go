package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// UserPreference holds the schema definition for the UserPreference entity.
type UserPreference struct {
	ent.Schema
}

// Fields of the UserPreference.
func (UserPreference) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("default_model", uuid.UUID{}).
			Comment("The default model to use for the user's new chats"),
		field.UUID("default_personality", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("The default personality to use for the user's new chats"),
		field.Enum("theme").
			Values("light", "dark", "system").
			Default("dark").
			Comment("The theme to use for the user's interface"),
		field.String("last_seen_announcement").
			Optional().
			Default("").
			Comment("ID of the most recently seen announcement banner; empty means none seen"),
		field.Bool("experimental_memory_dedupe_chain").
			Default(false).
			Comment("Deprecated unused column; memory merge/dedupe is always on. Kept until a follow-up migration drops it."),
		field.JSON("favorite_model_ids", []string{}).
			Default([]string{}).
			Comment("Model IDs the user has starred in the model picker, user-global (not per-personality).").
			Annotations(entsql.Default("[]")),
		// Subtractive layer *within* added_model_ids: a model the account keeps
		// on its list but does not want in the picker right now. Hiding is the
		// cheap, reversible half of the pair — removing costs a search to undo,
		// so the two are not redundant.
		//
		// A preference, not a permission. Hiding a model keeps it usable by
		// anything that names it directly — an existing chat pinned to it, an
		// agent job, the API — because unchecking a box in settings should not
		// silently break a scheduled job.
		field.JSON("hidden_model_ids", []string{}).
			Default([]string{}).
			Comment("Model IDs the user has hidden from their model picker, user-global. Visibility only; a hidden model still runs when named directly.").
			Annotations(entsql.Default("[]")),
		// The models this account has chosen to have available at all.
		//
		// This is a show list, which on its own would leave a model added to the
		// catalog dark until someone went looking. seen_model_ids is what makes
		// that survivable: anything this account has never been offered is added
		// for it, so nobody starts from an empty picker, nobody opts into the
		// models they already had, and a model added later still arrives.
		field.JSON("added_model_ids", []string{}).
			Default([]string{}).
			Comment("Model IDs on this account's own list. Empty for an account that has never been offered anything; see seen_model_ids.").
			Annotations(entsql.Default("[]")),
		// Every model this account has ever been offered.
		//
		// This is what tells "never seen it" apart from "removed it", and those
		// must not be confused in either direction. A model absent from here is
		// new and gets added; a model here but absent from added_model_ids was
		// removed on purpose and stays gone.
		//
		// Recording models rather than providers is load-bearing. Recording
		// providers would freeze an account's list at whatever the catalog held
		// the day it was first seeded, so a model added later — by an upgrade,
		// or by an operator — would never reach anyone who already had that
		// provider. Per-model, a genuinely new model arrives for everyone who
		// has not explicitly removed it.
		//
		// It outlives the provider key deliberately: keying this off the stored
		// key would mean removing and re-adding a key silently restored every
		// model the account had deliberately removed.
		field.JSON("seen_model_ids", []string{}).
			Default([]string{}).
			Comment("Model IDs this account has ever been offered. Distinguishes a new model from a removed one; a model here but not in added_model_ids was removed deliberately.").
			Annotations(entsql.Default("[]")),
	}
}

// Edges of the UserPreference.
func (UserPreference) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("preferences").
			Unique().
			Required(),
		edge.To("model", Model.Type).
			Field("default_model").
			Unique().
			Required(),
		edge.To("personality", Personality.Type).
			Field("default_personality").
			Unique(),
	}
}
