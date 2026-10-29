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

// AccountProviderKey is where accounts stored model-provider API keys before
// provider connections existed (ADR 0x027). Nothing writes it any more: at
// startup accountkeys.ConvertStoredKeys turns each row into a connection and
// removes it. It is removed together with that conversion (ADR 0x027).
//
// The edge to User is one-way. The foreign key and its cascade live on this
// table, so deleting an account deletes its keys, and the User schema does not
// change.
type AccountProviderKey struct {
	ent.Schema
}

func (AccountProviderKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("user_id", uuid.UUID{}).Immutable(),
		// A models.ModelProvider value. A string rather than an enum so that adding
		// a provider needs no migration; only known providers are accepted.
		field.String("provider").NotEmpty().Immutable(),
		// The key, encrypted at rest (token_crypto, "mcpv1:" ciphertext). It is
		// never returned over the API.
		field.String("key").NotEmpty().Sensitive(),
		// The key's last four characters, so the owner can tell which key is stored.
		field.String("hint").Default(""),
	}
}

func (AccountProviderKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("owner", User.Type).
			Field("user_id").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (AccountProviderKey) Indexes() []ent.Index {
	return []ent.Index{
		// One key per provider per account; setting a key replaces it.
		index.Fields("user_id", "provider").Unique(),
	}
}

func (AccountProviderKey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		// Written to the database, for anyone reading it there.
		schema.Comment("Superseded by provider_connections: rows here are converted into connections at startup and removed."),
		entsql.WithComments(true),
	}
}

func (AccountProviderKey) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
