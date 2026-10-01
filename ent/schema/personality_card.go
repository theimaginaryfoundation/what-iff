package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// PersonalityCard holds the verbatim `data` object of a character card (SillyTavern chara_card_v2/v3)
// a personality was imported from.
//
// It lives in its own table rather than a column on Personality on purpose: personalities are read
// on hot paths (every message send, every list page) and a card can carry megabytes of lore the
// agent never looks at. Only the card export and the account export ever load it.
type PersonalityCard struct {
	ent.Schema
}

// Fields of the PersonalityCard.
func (PersonalityCard) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.JSON("data", map[string]any{}).
			Comment("The original card `data` object, kept opaque so export can round-trip fields we do not model (extensions, tags, greetings, character_book, ...)."),
		field.JSON("omitted_fields", []string{}).
			Optional().
			Comment("Card fields (scenario, personality) import left out of the system prompt to fit the length limit; export restores them from data."),
	}
}

// Edges of the PersonalityCard.
func (PersonalityCard) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("personality", Personality.Type).
			Ref("card").
			Unique().
			Required(),
	}
}

// Mixin of the PersonalityCard.
func (PersonalityCard) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
