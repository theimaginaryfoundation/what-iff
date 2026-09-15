package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// UserProviderKey is one model-provider API key belonging to one account.
//
// Keys are per-account rather than per-deployment on purpose: a self-hosted
// instance with more than one person has more than one person's credentials
// and more than one person's bill. A deployment-wide key would also make this
// an operator setting, which is the shape an admin surface has — and the point
// of putting keys in the integrations screen is that an ordinary user can add
// their own without one.
//
// The deployment-level environment variables remain a fallback for a single
// user who would rather configure the file, and for existing installs.
type UserProviderKey struct {
	ent.Schema
}

// Fields of the UserProviderKey.
func (UserProviderKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		// Matches models.ModelProvider ("openai", "anthropic", ...). A plain
		// string rather than an enum so adding a provider to the catalog does
		// not require a schema migration.
		field.String("provider").
			NotEmpty().
			MaxLen(40).
			Immutable(),
		// Encrypted at rest with TOKEN_ENCRYPTION_SECRET, the same treatment
		// MCPServer.auth_token gets. Sensitive() keeps it out of ent's
		// generated String() output so it cannot be logged by accident.
		field.String("encrypted_key").
			NotEmpty().
			Sensitive(),
		// Enough to show "sk-…4f2a" in the UI without holding the key: a user
		// managing several accounts needs to tell them apart, and round-trips
		// of the real value exist only to set it.
		field.String("key_hint").
			Optional().
			MaxLen(12),
	}
}

// Edges of the UserProviderKey.
func (UserProviderKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("provider_keys").
			Unique().
			Required(),
	}
}

// Indexes of the UserProviderKey.
func (UserProviderKey) Indexes() []ent.Index {
	return []ent.Index{
		// One key per provider per account: setting a key replaces the
		// previous one rather than accumulating rows nothing would choose
		// between.
		index.Edges("owner").Fields("provider").Unique(),
	}
}

// Mixin of the UserProviderKey.
func (UserProviderKey) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
