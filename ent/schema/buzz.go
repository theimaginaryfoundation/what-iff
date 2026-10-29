package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Buzz relay: a persona is a member of Buzz communities under its own Nostr key, held by the
// server; bindings tie the community channels it is in to relay threads; message links record
// every message that crossed between the two. See docs/adr/0x028-buzz-relay.md and
// internal/buzzrelay.
//
// As for Discord, the persona and the relay thread are plain UUID columns rather than edges,
// so the Personality and Chat schemas stay unaware of the relay. The owner edge is
// User.buzz_agents.

// BuzzAgent is a persona's Buzz identity: one secp256k1 keypair per persona.
type BuzzAgent struct {
	ent.Schema
}

func (BuzzAgent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The persona this identity speaks as. One per persona.
		field.UUID("personality_id", uuid.UUID{}),
		// The x-only public key, lowercase hex: what Buzz knows the persona by.
		field.String("pubkey").NotEmpty().Immutable(),
		// The secret key, encrypted at rest (token_crypto, "mcpv1:" ciphertext).
		field.String("secret_key").NotEmpty().Sensitive(),
		field.Enum("status").Values("active", "disabled").Default("active"),
		field.String("last_error").Optional().Nillable(),
	}
}

func (BuzzAgent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("buzz_agents").Unique().Required(),
		edge.To("communities", BuzzCommunity.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BuzzAgent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("personality_id").Unique(),
		index.Fields("pubkey").Unique(),
		index.Edges("owner"),
	}
}

func (BuzzAgent) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// BuzzCommunity is one community a persona is a member of (or is trying to be): a relay URL,
// which in Buzz selects the community.
type BuzzCommunity struct {
	ent.Schema
}

func (BuzzCommunity) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The community's WebSocket URL, normalised (wss://host[:port]).
		field.String("relay_url").NotEmpty().Immutable(),
		// The relay's NIP-11 name, cached on add and on every check.
		field.String("name").Default(""),
		// The relay's own pubkey (it signs channel state), cached from NIP-11.
		field.String("relay_pubkey").Default(""),
		// The owner's NIP-OA attestation for the persona's key, verified, as the JSON tag.
		field.String("auth_tag").Optional().Nillable(),
		field.String("owner_pubkey").Optional().Nillable(),
		// The join terms version the owner accepted for the persona, when the community has terms.
		field.String("policy_version").Optional().Nillable(),
		field.Enum("status").Values("active", "needs_access", "error", "disabled").Default("needs_access"),
		field.String("last_error").Optional().Nillable(),
		field.Time("last_connected_at").Optional().Nillable(),
		// The newest event the gateway has seen from the community, so a fresh connection (a
		// deploy, a new gateway leader, a changed binding) resumes from there rather than now.
		field.Time("last_event_at").Optional().Nillable(),
	}
}

func (BuzzCommunity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("agent", BuzzAgent.Type).Ref("communities").Unique().Required(),
		edge.To("bindings", BuzzBinding.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BuzzCommunity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("relay_url").Edges("agent").Unique(),
	}
}

func (BuzzCommunity) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// BuzzBinding binds one channel of a community to a relay thread.
type BuzzBinding struct {
	ent.Schema
}

func (BuzzBinding) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The relay thread.
		field.UUID("chat_id", uuid.UUID{}),
		// The NIP-29 group id (Buzz's h tag).
		field.String("channel_id").NotEmpty(),
		// Display name, cached when the binding is made or refreshed.
		field.String("channel_name").Default(""),
		// Whether tagging the persona in the channel starts a turn.
		field.Bool("inbound_enabled").Default(true),
		// Pubkeys (hex). Empty allow list = anyone not denied; deny always wins.
		field.JSON("allow_pubkeys", []string{}).Default([]string{}),
		field.JSON("deny_pubkeys", []string{}).Default([]string{}),
		// Whether other agents (events carrying a NIP-OA attestation) may start turns.
		// Off by default: agent-to-agent conversation and its loop guards are a follow-up.
		field.Bool("answer_agents").Default(false),
		// As for Discord: the owner's acknowledgement that the thread is NOT sandboxed, only
		// ever recorded while it is not, so the relay fails closed.
		field.Bool("allow_unrestricted").Default(false),
		field.Enum("status").Values("active", "broken").Default("active"),
		field.String("last_error").Optional().Nillable(),
		field.Time("last_activity_at").Optional().Nillable(),
	}
}

func (BuzzBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("community", BuzzCommunity.Type).Ref("bindings").Unique().Required(),
		edge.To("links", BuzzMessageLink.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("pending_posts", BuzzPendingPost.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (BuzzBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_id").Edges("community").Unique(),
		index.Fields("chat_id"),
	}
}

func (BuzzBinding) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// BuzzMessageLink is one message that crossed between a relay thread and its channel: an
// inbound event saved as a user message, or a reply published out.
type BuzzMessageLink struct {
	ent.Schema
}

func (BuzzMessageLink) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.Enum("direction").Values("inbound", "outbound"),
		// The What Iff message: the saved user message (inbound) or the reply (outbound).
		field.UUID("chat_message_id", uuid.UUID{}).Optional().Nillable(),
		// Inbound: the event that triggered the turn. Unique per binding, so a redelivered
		// event (reconnect, leader handover) is answered once.
		field.String("event_id").Optional().Nillable(),
		// The thread the message is in (its root event), for replying in place.
		field.String("thread_root").Default(""),
		// Outbound: the events the reply was published as.
		field.JSON("posted_event_ids", []string{}).Default([]string{}),
		// Inbound provenance: the author's signed identity and their display name then.
		field.String("author_pubkey").Default(""),
		field.String("author_name").Default(""),
		// Outbound: the inbound link this reply answers, if it was triggered from Buzz.
		field.UUID("reply_to_link_id", uuid.UUID{}).Optional().Nillable(),
		field.Enum("status").Values("received", "pending", "sent", "failed").Default("received"),
		field.String("error").Optional().Nillable(),
		field.Int("attempts").Default(0),
	}
}

func (BuzzMessageLink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("binding", BuzzBinding.Type).Ref("links").Unique().Required(),
	}
}

func (BuzzMessageLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("event_id").Edges("binding").Unique(),
		index.Fields("chat_message_id"),
	}
}

func (BuzzMessageLink) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// BuzzPendingPost asks for the next reply in a chat to be posted to a binding's channel: set
// by the composer toggle or the post_to_buzz tool, consumed by the reply hook.
type BuzzPendingPost struct {
	ent.Schema
}

func (BuzzPendingPost) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("chat_id", uuid.UUID{}),
		field.Enum("source").Values("composer", "tool"),
		field.Time("expires_at").Default(func() time.Time { return time.Now().Add(30 * time.Minute) }),
	}
}

func (BuzzPendingPost) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("binding", BuzzBinding.Type).Ref("pending_posts").Unique().Required(),
	}
}

func (BuzzPendingPost) Indexes() []ent.Index {
	return []ent.Index{index.Fields("chat_id")}
}

func (BuzzPendingPost) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
