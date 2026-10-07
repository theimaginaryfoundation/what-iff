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

// Discord relay: a user's own Discord bot speaks as one of their personas; bindings
// tie the bot's channels to relay threads; message links record every message that
// crossed between the two. See docs/adr/0x025-discord-relay.md and
// internal/discordrelay.
//
// The persona and the relay thread are plain UUID columns rather than edges, so the
// Personality and Chat schemas stay unaware of the relay; the relay treats a persona
// or thread that has since been deleted as a broken binding. The owner edge is
// User.discord_bots.

// DiscordBot is a Discord application's bot user, brought by its owner and
// attached to one of their personas.
type DiscordBot struct {
	ent.Schema
}

func (DiscordBot) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The persona the bot speaks as. One bot per persona.
		field.UUID("personality_id", uuid.UUID{}),
		field.String("application_id").NotEmpty(),
		// Discord's id for the bot user; what mentions and authorship are matched on.
		field.String("bot_user_id").NotEmpty(),
		field.String("bot_username").Default(""),
		// The bot token, encrypted at rest (token_crypto, "mcpv1:" ciphertext).
		field.String("token").NotEmpty().Sensitive(),
		// Whether the application has the privileged Message Content intent on, so
		// the gateway may request it (needed only for triggers other than mentions).
		field.Bool("message_content").Default(false),
		field.Enum("status").Values("active", "invalid_token", "disabled").Default("active"),
		field.String("last_error").Optional().Nillable(),
	}
}

func (DiscordBot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("discord_bots").Unique().Required(),
		edge.To("bindings", DiscordBinding.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (DiscordBot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("personality_id").Unique(),
		// A Discord bot can be connected by one account only.
		index.Fields("bot_user_id").Unique(),
		index.Edges("owner"),
	}
}

func (DiscordBot) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// DiscordBinding binds one Discord channel, seen by one bot, to a relay thread.
type DiscordBinding struct {
	ent.Schema
}

func (DiscordBinding) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The relay thread.
		field.UUID("chat_id", uuid.UUID{}),
		field.String("guild_id").NotEmpty(),
		field.String("channel_id").NotEmpty(),
		// Display names, cached when the binding is made or refreshed.
		field.String("guild_name").Default(""),
		field.String("channel_name").Default(""),
		// Whether tagging the bot in the channel starts a turn.
		field.Bool("inbound_enabled").Default(true),
		// Discord user ids. Empty allow list = anyone not denied; deny always wins.
		field.JSON("allow_user_ids", []string{}).Default([]string{}),
		field.JSON("deny_user_ids", []string{}).Default([]string{}),
		// The owner's explicit acknowledgement that this relay thread is NOT sandboxed,
		// so anyone allowed to tag the bot can use its full memory, scratchpad and tools.
		// Default false: the relay answers only sandboxed threads unless this is set (fail
		// closed), and it is only ever recorded while the thread is not sandboxed, so
		// switching a thread's sandbox off later pauses the binding instead of silently
		// widening it.
		field.Bool("allow_unrestricted").Default(false),
		field.Enum("status").Values("active", "broken").Default("active"),
		field.String("last_error").Optional().Nillable(),
		field.Time("last_activity_at").Optional().Nillable(),
	}
}

func (DiscordBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("bot", DiscordBot.Type).Ref("bindings").Unique().Required(),
		edge.To("links", DiscordMessageLink.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("pending_posts", DiscordPendingPost.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (DiscordBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_id").Edges("bot").Unique(),
		index.Fields("chat_id"),
	}
}

func (DiscordBinding) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// DiscordMessageLink is one message that crossed between a relay thread and its
// channel: an inbound Discord message saved as a user message, or a reply posted
// out. It is the provenance of inbound messages, the delivery record of outbound
// ones, and the dedupe key for both.
type DiscordMessageLink struct {
	ent.Schema
}

func (DiscordMessageLink) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.Enum("direction").Values("inbound", "outbound"),
		// The What Iff message: the saved user message (inbound) or the reply (outbound).
		// Nil only briefly, for an inbound message whose turn has not been saved yet.
		field.UUID("chat_message_id", uuid.UUID{}).Optional().Nillable(),
		// Inbound: the Discord message that triggered the turn. Unique per binding,
		// so a redelivered event (reconnect, leader handover) is answered once.
		field.String("discord_message_id").Optional().Nillable(),
		// Where the message is in Discord (the Discord thread, when in one).
		field.String("discord_channel_id").Default(""),
		// Outbound: the Discord messages the reply was split into.
		field.JSON("posted_message_ids", []string{}).Default([]string{}),
		// Inbound provenance.
		field.String("author_id").Default(""),
		field.String("author_name").Default(""),
		// Outbound: the inbound link this reply answers, if it was triggered from Discord.
		field.UUID("reply_to_link_id", uuid.UUID{}).Optional().Nillable(),
		field.Enum("status").Values("received", "pending", "sent", "failed").Default("received"),
		field.String("error").Optional().Nillable(),
		field.Int("attempts").Default(0),
	}
}

func (DiscordMessageLink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("binding", DiscordBinding.Type).Ref("links").Unique().Required(),
	}
}

func (DiscordMessageLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("discord_message_id").Edges("binding").Unique(),
		index.Fields("chat_message_id"),
	}
}

func (DiscordMessageLink) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// DiscordPendingPost asks for the next reply in a chat to be posted to a binding's
// channel: set by the composer toggle or the post_to_discord tool, consumed by the
// reply hook. It expires so a forgotten toggle does not post a much later reply.
type DiscordPendingPost struct {
	ent.Schema
}

func (DiscordPendingPost) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("chat_id", uuid.UUID{}),
		field.Enum("source").Values("composer", "tool"),
		field.Time("expires_at").Default(func() time.Time { return time.Now().Add(30 * time.Minute) }),
	}
}

func (DiscordPendingPost) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("binding", DiscordBinding.Type).Ref("pending_posts").Unique().Required(),
	}
}

func (DiscordPendingPost) Indexes() []ent.Index {
	return []ent.Index{index.Fields("chat_id")}
}

func (DiscordPendingPost) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
