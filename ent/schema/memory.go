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

// Memory holds the schema definition for the Memory entity.
type Memory struct {
	ent.Schema
}

type MemoryChainMetadata struct {
	DuplicateCount          int         `json:"duplicate_count"`
	VerifiedTimestampsFirst []time.Time `json:"verified_timestamps_first,omitempty"`
	VerifiedTimestampsLast  []time.Time `json:"verified_timestamps_last,omitempty"`
	MergedFromMemoryIDs     []uuid.UUID `json:"merged_from_memory_ids,omitempty"`
}

// Fields of the Memory.
func (Memory) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Text("content").
			NotEmpty().
			Comment("The actual memory content"),
		field.Enum("scope").
			Values("User", "Chat", "Summary").
			Comment("Whether the memory is user-global, chat-specific, or the singleton summary for a chat"),
		field.Enum("type").
			Values("Context").
			Default("Context").
			Comment("Memory type classification for API-level grouping"),
		field.Enum("status").
			Values("active", "inactive").
			Default("active").
			Comment("Lifecycle status for soft-unlisting merged/obsolete memories"),
		field.Float("confidence").
			Default(0.6).
			Min(0).
			Max(1).
			Comment("Stored confidence in [0,1]. The extraction/merge LLM emits coarse buckets mapped to anchors (low=0.3, medium=0.6, high=0.9); other signals (e.g. the reconfirmation tally) can refine it to finer values."),
		field.JSON("chain_metadata", &MemoryChainMetadata{}).
			Optional().
			Comment("Chain-of-trust metadata for merged memory lineages"),
		field.UUID("link_group_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Groups related-but-distinct memories that share a topic/event across different functional registers (technical/emotional/narrative/etc). Unlike a merge, linked members keep ALL their surfaces and stay active + searchable; this only cross-references them. V1: a memory belongs to at most one link group."),
		field.Bool("starred").
			Default(false).
			Comment("Whether the memory is starred by the user"),
		field.UUID("pinned_personality_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("Optional personality ID to pin this User-scoped memory to. When set, only this personality can access the memory. When null, all personalities can access it."),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Annotations(entsql.Default("CURRENT_TIMESTAMP")),
	}
}

// Edges of the Memory.
func (Memory) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("memories").
			Unique().
			Required(),
		edge.From("chat", Chat.Type).
			Ref("memories").
			Unique(),
		edge.To("pinned_personality", Personality.Type).
			Unique().
			Field("pinned_personality_id"),
	}
}

// Indexes of the Memory.
func (Memory) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner").Fields("scope"),
		index.Fields("pinned_personality_id"),
		index.Fields("link_group_id"),
	}
}
