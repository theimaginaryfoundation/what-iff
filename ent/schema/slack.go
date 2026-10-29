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

// Slack relay: a persona is a member of a Slack workspace as its own Slack app, whose tokens
// the server holds; bindings tie the channels it is in to relay threads; message links record
// every message that crossed between the two. See docs/adr/0x032-slack-relay.md and
// internal/slackrelay.
//
// As for Discord and Buzz, the persona and the relay thread are plain UUID columns rather than
// edges, so the Personality and Chat schemas stay unaware of the relay. The owner edge is
// User.slack_apps.

// SlackApp is a persona's Slack app in one workspace: its bot token and its Socket Mode
// app-level token.
type SlackApp struct {
	ent.Schema
}

func (SlackApp) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The persona this app speaks as.
		field.UUID("personality_id", uuid.UUID{}),
		// The workspace the app is installed in, and its cached name and URL.
		field.String("team_id").NotEmpty().Immutable(),
		field.String("team_name").Default(""),
		field.String("team_url").Default(""),
		// The Slack app (A…). One What Iff connection per app: Slack spreads an app's events
		// over all of its Socket Mode connections.
		field.String("app_id").NotEmpty().Immutable(),
		// The app's bot user (U…, what a mention names) and bot id (B…, on its messages).
		field.String("bot_user_id").NotEmpty().Immutable(),
		field.String("bot_id").Default(""),
		field.String("bot_name").Default(""),
		// The bot token (xoxb-) and the app-level token (xapp-), encrypted at rest
		// (token_crypto, "mcpv1:" ciphertext).
		field.String("bot_token").NotEmpty().Sensitive(),
		field.String("app_token").NotEmpty().Sensitive(),
		field.Enum("status").Values("active", "invalid_token", "disabled").Default("active"),
		field.String("last_error").Optional().Nillable(),
		field.Time("last_connected_at").Optional().Nillable(),
	}
}

func (SlackApp) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("slack_apps").Unique().Required(),
		edge.To("bindings", SlackBinding.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SlackApp) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("app_id").Unique(),
		// One app per persona per workspace.
		index.Fields("team_id", "personality_id").Unique(),
		index.Fields("personality_id"),
		index.Edges("owner"),
	}
}

func (SlackApp) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// SlackBinding binds one channel of the app's workspace to a relay thread.
type SlackBinding struct {
	ent.Schema
}

func (SlackBinding) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The relay thread.
		field.UUID("chat_id", uuid.UUID{}),
		// The Slack channel id (C… or G…).
		field.String("channel_id").NotEmpty(),
		// Display name, cached when the binding is made and on renames.
		field.String("channel_name").Default(""),
		field.Bool("is_private").Default(false),
		// Whether mentioning the persona in the channel starts a turn.
		field.Bool("inbound_enabled").Default(true),
		// Slack user ids (U…). Empty allow list = anyone not denied; deny always wins.
		field.JSON("allow_user_ids", []string{}).Default([]string{}),
		field.JSON("deny_user_ids", []string{}).Default([]string{}),
		// Whether other apps' bots may start turns, behind the loop guard (docs/adr/0x031).
		field.Bool("answer_agents").Default(false),
		// As for Discord and Buzz: the owner's acknowledgement that the thread is NOT
		// sandboxed, only ever recorded while it is not, so the relay fails closed.
		field.Bool("allow_unrestricted").Default(false),
		field.Enum("status").Values("active", "broken").Default("active"),
		field.String("last_error").Optional().Nillable(),
		field.Time("last_activity_at").Optional().Nillable(),
	}
}

func (SlackBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("app", SlackApp.Type).Ref("bindings").Unique().Required(),
		edge.To("links", SlackMessageLink.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("pending_posts", SlackPendingPost.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SlackBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_id").Edges("app").Unique(),
		index.Fields("chat_id"),
	}
}

func (SlackBinding) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// SlackMessageLink is one message that crossed between a relay thread and its channel: an
// inbound message saved as a user message, or a reply posted out.
type SlackMessageLink struct {
	ent.Schema
}

func (SlackMessageLink) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.Enum("direction").Values("inbound", "outbound"),
		// The What Iff message: the saved user message (inbound) or the reply (outbound).
		field.UUID("chat_message_id", uuid.UUID{}).Optional().Nillable(),
		// Inbound: the ts of the message that triggered the turn. Unique per binding, so a
		// redelivered event (Slack's retries, a leader handover) is answered once.
		field.String("message_ts").Optional().Nillable(),
		// The thread the message is in (its first message's ts), for replying in place.
		field.String("thread_ts").Default(""),
		// Outbound: the ts of each message the reply was posted as.
		field.JSON("posted_ts", []string{}).Default([]string{}),
		// Inbound provenance: the author's Slack user id and their display name then.
		field.String("author_user_id").Default(""),
		field.String("author_name").Default(""),
		// Outbound: the inbound link this reply answers, if it was triggered from Slack.
		field.UUID("reply_to_link_id", uuid.UUID{}).Optional().Nillable(),
		field.Enum("status").Values("received", "pending", "sent", "failed").Default("received"),
		field.String("error").Optional().Nillable(),
		field.Int("attempts").Default(0),
		// Inbound: how many turns in a row agents have started on the binding, this one
		// included (0 when a person wrote it). The loop guard reads the newest (docs/adr/0x031).
		field.Int("agent_depth").Default(0),
	}
}

func (SlackMessageLink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("binding", SlackBinding.Type).Ref("links").Unique().Required(),
	}
}

func (SlackMessageLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("message_ts").Edges("binding").Unique(),
		index.Fields("chat_message_id"),
		// Thread continuations: the binding's links in one thread.
		index.Fields("thread_ts").Edges("binding"),
		// The binding's newest inbound message within the loop guard's chain gap.
		index.Fields("created_at").Edges("binding"),
	}
}

func (SlackMessageLink) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// SlackPendingPost asks for the next reply in a chat to be posted to a binding's channel: set
// by the composer toggle or the post_to_slack tool, consumed by the reply hook.
type SlackPendingPost struct {
	ent.Schema
}

func (SlackPendingPost) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("chat_id", uuid.UUID{}),
		field.Enum("source").Values("composer", "tool"),
		field.Time("expires_at").Default(func() time.Time { return time.Now().Add(30 * time.Minute) }),
	}
}

func (SlackPendingPost) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("binding", SlackBinding.Type).Ref("pending_posts").Unique().Required(),
	}
}

func (SlackPendingPost) Indexes() []ent.Index {
	return []ent.Index{index.Fields("chat_id")}
}

func (SlackPendingPost) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
