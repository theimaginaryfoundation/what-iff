package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

// CompactionLoadedMemory is one memory that was live in the compacted conversation segment
// (prefetched or pulled in via find_context). Captured so the audit shows exactly what context
// had accumulated at compaction time — the accumulation the merger is meant to compact.
type CompactionLoadedMemory struct {
	MemoryID   *uuid.UUID `json:"memory_id,omitempty"`
	Content    string     `json:"content"`
	Scope      string     `json:"scope"`
	Confidence float64    `json:"confidence,omitempty"`
}

// CompactionEvent is the append-only audit record for one conversation checkpoint ("compaction").
// It treats a compaction like a PR: it captures the before/after of both the conversation summary
// and the personality scratchpad (via FK-referenced CheckpointSnapshot rows), the full set of loaded
// memories, memories newly created during the compaction, and — through the merge_events edge —
// every merge/link that changed *existing* agent state. Purely additive: the live summary/scratchpad
// still live on Chat/Personality; nothing here changes the read path.
type CompactionEvent struct {
	ent.Schema
}

func (CompactionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("user_id", uuid.UUID{}).
			Comment("Owner"),
		field.UUID("chat_id", uuid.UUID{}).
			Comment("Thread that was compacted"),
		field.UUID("personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Active personality at compaction time (the scratchpad owner)"),
		field.UUID("assistant_message_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Assistant message whose turn tripped the checkpoint"),
		field.String("provider").
			Optional().
			Comment("Archival provider family that ran the checkpoint: OpenAI or Claude"),
		field.String("reason").
			Optional().
			Comment("Checkpoint decision reason (token budget / message count / etc.)"),
		field.Text("summary_explanation").
			Optional().
			Comment("Short model-generated explanation of what changed in the conversation summary"),
		field.Text("scratchpad_explanation").
			Optional().
			Comment("Short model-generated explanation of what changed in the personality scratchpad"),
		// FK columns to CheckpointSnapshot, surfaced through the edges below.
		field.UUID("old_summary_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("new_summary_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("old_scratchpad_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("new_scratchpad_id", uuid.UUID{}).Optional().Nillable(),
		field.JSON("loaded_memories", []CompactionLoadedMemory{}).
			Optional().
			Comment("Snapshot of every memory loaded into the compacted segment (prefetch + find_context)"),
		field.JSON("created_memories", []CompactionLoadedMemory{}).
			Optional().
			Comment("Memories newly created during this compaction (singleton extractions or all-new collapses); not merge events"),
	}
}

func (CompactionEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("old_summary", CheckpointSnapshot.Type).
			Unique().
			Field("old_summary_id"),
		edge.To("new_summary", CheckpointSnapshot.Type).
			Unique().
			Field("new_summary_id"),
		edge.To("old_scratchpad", CheckpointSnapshot.Type).
			Unique().
			Field("old_scratchpad_id"),
		edge.To("new_scratchpad", CheckpointSnapshot.Type).
			Unique().
			Field("new_scratchpad_id"),
		// Merge/link events that changed existing memories. FK lives on MemoryMergeEvent
		// (compaction_event_id); SetNull so pruning a compaction event never destroys merge history.
		edge.To("merge_events", MemoryMergeEvent.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (CompactionEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
		index.Fields("chat_id"),
	}
}

func (CompactionEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
