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

// ProviderConnection is one way to reach one model provider: a built-in
// provider, a known third party, or any compatible server (ADR 0x027). It
// belongs to an account, or to the deployment when it has no owner.
//
// The edge to User is one-way. The foreign key and its cascade live on this
// table, so deleting an account deletes its connections, and the User schema
// does not change.
type ProviderConnection struct {
	ent.Schema
}

func (ProviderConnection) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The owning account; empty for the deployment's own connection.
		field.UUID("user_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		// A built-in provider (a models.ModelProvider value) or another preset. A
		// string rather than an enum so that adding one needs no migration.
		field.String("preset").NotEmpty().Immutable(),
		// The API a custom connection speaks. Empty for a preset, which implies it.
		field.String("flavor").Default(""),
		field.String("name").NotEmpty(),
		// Empty means the preset's own address.
		field.String("base_url").Default(""),
		// The key, encrypted at rest (token_crypto, "mcpv1:" ciphertext), or empty
		// for a server that needs none. It is never returned over the API.
		field.String("key").Default("").Sensitive(),
		// The key's last four characters, so the owner can tell which key is stored.
		field.String("hint").Default(""),
		// Where the connection came from, when it was not added in the app:
		// "converted" for a key stored before connections existed, so the app can
		// tell those accounts once. Empty for one added in the app.
		field.String("source").Default("").Immutable(),
		// Set to the preset for a built-in provider and empty otherwise, so the
		// unique index below allows one built-in connection of each kind per
		// account. Requests to a built-in provider ask for "the account's key",
		// which needs exactly one answer until models point at connections.
		field.String("builtin").Optional().Nillable().Immutable(),
	}
}

func (ProviderConnection) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("owner", User.Type).
			Field("user_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (ProviderConnection) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "builtin").Unique(),
	}
}

func (ProviderConnection) Annotations() []schema.Annotation {
	return []schema.Annotation{
		// Written to the database, for anyone reading it there.
		schema.Comment("How accounts and the deployment reach model providers. A row with no user_id belongs to the deployment."),
		entsql.WithComments(true),
	}
}

func (ProviderConnection) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
