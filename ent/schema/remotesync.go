package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Remote sync: a self-hosted server keeps a copy of its owner's threads on another What Iff
// server (the hosted app, usually), pulled through that server's webhook read routes with a
// chat:read token. See docs/adr/0x033-mullet-mode-remote-sync.md and internal/remotesync.
//
// Local threads and messages are plain UUID columns rather than edges, so the Chat and
// ChatMessage schemas stay unaware of sync; a link whose local thread has been deleted is
// detached rather than repaired. The owner edge is User.remote_syncs.

// RemoteSync is one user's sync settings: where to pull from, with which token, and how the
// last run went. One per user.
type RemoteSync struct {
	ent.Schema
}

func (RemoteSync) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// The remote server's origin, e.g. https://whatiff.chat (no /api suffix).
		field.String("base_url").NotEmpty(),
		// The remote wht_ token, encrypted at rest (token_crypto, "mcpv1:" ciphertext).
		field.String("token").NotEmpty().Sensitive(),
		// The token's last four characters, so the settings page can show which one is stored.
		field.String("token_hint").Default(""),
		field.Bool("enabled").Default(true),
		// pending: never run; ok: last run succeeded; error: last run failed and will be retried;
		// needs_attention: the remote refused the token (401/403), so runs stop until the
		// settings are saved again.
		field.Enum("status").Values("pending", "ok", "error", "needs_attention").Default("pending"),
		field.Text("last_error").Optional().Nillable(),
		field.Time("last_run_at").Optional().Nillable(),
		field.Time("last_success_at").Optional().Nillable(),
		// Counts from the last run (models.RemoteSyncRunStats); never content.
		field.JSON("last_run", map[string]int{}).Optional(),
		// A run holds the account until this time, so two instances (or a timer tick and
		// "sync now") never run it at once. Cleared when the run ends.
		field.Time("lease_until").Optional().Nillable(),
		// Which run holds lease_until. Each claim sets a new one, and only that run may extend,
		// finish or release the lease, so a run that stalled past its lease and was overtaken
		// cannot extend or clear the lease of the run that took over.
		field.UUID("lease_id", uuid.UUID{}).Optional().Nillable(),
	}
}

func (RemoteSync) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("remote_syncs").Unique().Required(),
		edge.To("chats", RemoteSyncChat.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (RemoteSync) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner").Unique(),
	}
}

func (RemoteSync) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// RemoteSyncChat links one remote thread to the local thread currently tracking it.
type RemoteSyncChat struct {
	ent.Schema
}

func (RemoteSyncChat) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("remote_chat_id", uuid.UUID{}),
		// The local thread new remote messages go to. A fork moves it to the new thread.
		field.UUID("chat_id", uuid.UUID{}),
		// The remote thread's name as last seen, so a remote rename is copied only while the
		// local name is still the remote one.
		field.String("remote_name").Default(""),
		// The remote thread's persona name. A copy whose local thread has no persona is matched to
		// a local persona of this name whenever one appears (created here, or by an account import).
		field.String("remote_personality_name").Default(""),
		// The remote thread's last_message_time as last seen: a different value means it changed.
		field.Time("remote_last_message_time").Optional().Nillable(),
		// The newest remote message copied, where the next pull stops (less an overlap).
		field.Time("remote_head_sent_at").Optional().Nillable(),
		// How many times this remote thread forked locally, and the thread the last fork came from.
		field.Int("forks").Default(0),
		field.UUID("forked_from_chat_id", uuid.UUID{}).Optional().Nillable(),
		// detached: the local thread was deleted, so the remote thread is no longer copied.
		field.Enum("state").Values("active", "detached").Default("active"),
	}
}

func (RemoteSyncChat) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("sync", RemoteSync.Type).Ref("chats").Unique().Required(),
		edge.To("messages", RemoteSyncMessage.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (RemoteSyncChat) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("remote_chat_id").Edges("sync").Unique(),
		index.Fields("chat_id"),
	}
}

func (RemoteSyncChat) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

// RemoteSyncMessage records one remote message copied into one local thread. It is the dedupe
// key (a remote message is copied into a thread at most once) and the provenance of the local
// message: a message with a link came from the remote; one without was written here.
type RemoteSyncMessage struct {
	ent.Schema
}

func (RemoteSyncMessage) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("chat_id", uuid.UUID{}),
		field.UUID("chat_message_id", uuid.UUID{}),
		field.UUID("remote_message_id", uuid.UUID{}),
		field.Time("remote_sent_at"),
	}
}

func (RemoteSyncMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("link", RemoteSyncChat.Type).Ref("messages").Unique().Required(),
	}
}

func (RemoteSyncMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chat_id", "remote_message_id").Unique(),
		index.Fields("chat_message_id"),
	}
}

func (RemoteSyncMessage) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }
