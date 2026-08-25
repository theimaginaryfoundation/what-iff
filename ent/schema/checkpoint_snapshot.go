package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

// CheckpointSnapshot is a content-addressed capture of a conversation summary or a personality
// scratchpad at a single checkpoint. Rows are deduplicated by (user, kind, owner-ref, content_hash)
// so the "new" snapshot written by one compaction is the SAME row referenced as the "old" snapshot
// by the next compaction — the old/new pair on every CompactionEvent costs at most one new row per
// checkpoint instead of duplicating the full text twice.
//
// This is also the append-only successor to Personality.scratchpad_history (a lossy rolling last-10
// window): every distinct summary/scratchpad state a user has ever checkpointed is retained here and
// can be reverted to.
type CheckpointSnapshot struct {
	ent.Schema
}

func (CheckpointSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("user_id", uuid.UUID{}).
			Comment("Owner of the snapshot"),
		field.Enum("kind").
			Values("summary", "scratchpad").
			Comment("summary = a Chat.checkpoint_summary capture; scratchpad = a Personality.scratchpad capture"),
		field.UUID("chat_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("For summary snapshots: the chat whose checkpoint summary this captures"),
		field.UUID("personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("For scratchpad snapshots: the personality whose scratchpad this captures (scratchpads are per-personality, shared across that personality's chats)"),
		field.Text("content").
			Comment("Snapshot content; may be empty for an initial/blank checkpoint"),
		field.String("content_hash").
			Comment("sha256 hex of content; part of the dedup key so identical consecutive states reuse a single row"),
	}
}

// Indexes support (a) the content-addressed get-or-create lookup and (b) listing a user's snapshots
// by kind. The lookup index is intentionally NOT unique: chat_id / personality_id are nullable and
// Postgres treats NULLs as distinct in unique indexes, which would defeat dedup on the null side.
// Checkpoints for a given chat are serialized, so get-or-create-by-query is race-safe enough; a rare
// duplicate row is harmless (any FK still points at a valid, correct snapshot).
func (CheckpointSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "kind", "chat_id", "personality_id", "content_hash"),
		index.Fields("user_id", "kind"),
	}
}

func (CheckpointSnapshot) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
