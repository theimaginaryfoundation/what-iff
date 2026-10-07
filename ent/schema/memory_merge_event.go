package schema

import (
	"encoding/json"
	"fmt"
	"strconv"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// MemoryMergeSourceMember is one row in the pre-merge snapshot stored on a merge event.
type MemoryMergeSourceMember struct {
	Content    string     `json:"content"`
	Scope      string     `json:"scope"`
	Confidence string     `json:"confidence,omitempty"`
	MemoryID   *uuid.UUID `json:"memory_id,omitempty"`
	IsNew      bool       `json:"is_new"`
}

// MemoryMergeUndoSnapshotVersion is the current MemoryMergeUndoSnapshot.Version. Rows written
// before the field existed decode as 0 ("legacy"): they carry no prior content/embedding and no
// explicit absorbed-member list, so undo falls back to source_members for the absorbed set.
const MemoryMergeUndoSnapshotVersion = 1

// MemoryMergeAbsorbedMember is one memory a fold retired (set inactive) into the survivor.
type MemoryMergeAbsorbedMember struct {
	MemoryID    uuid.UUID `json:"memory_id"`
	PriorStatus string    `json:"prior_status"`
}

// MemoryMergeUndoSnapshot captures enough state to revert an in-place fold. Every field added
// after the original three is optional, so legacy rows still decode (see Version).
type MemoryMergeUndoSnapshot struct {
	PriorConfidence          float64              `json:"prior_confidence,omitempty"`
	PriorChainMetadata       *MemoryChainMetadata `json:"prior_chain_metadata,omitempty"`
	PriorChainMetadataWasNil bool                 `json:"prior_chain_metadata_was_nil"`

	// Version is MemoryMergeUndoSnapshotVersion for rows written by current code, 0 for legacy rows.
	Version int `json:"version,omitempty"`
	// CanonicalContent is the merger's proposed phrasing for the group, recorded even when the
	// survivor was not rewritten (e.g. it is starred) so the audit row keeps the proposal.
	CanonicalContent string `json:"canonical_content,omitempty"`
	// ContentRewritten is true when the fold replaced the survivor's content with CanonicalContent
	// and re-embedded it. PriorContent and PriorEmbedding/PriorEmbeddingMissing are then set.
	ContentRewritten bool   `json:"content_rewritten,omitempty"`
	PriorContent     string `json:"prior_content,omitempty"`
	// PriorEmbedding is the survivor's embedding vector before the rewrite, so undo can restore it
	// without calling the embedding API. PriorEmbeddingMissing is true when the survivor had no
	// embedding row at all (undo then removes the one the rewrite created).
	PriorEmbedding        []float32 `json:"prior_embedding,omitempty"`
	PriorEmbeddingMissing bool      `json:"prior_embedding_missing,omitempty"`
	// AbsorbedMembers lists every memory the fold set inactive, with its status beforehand.
	AbsorbedMembers []MemoryMergeAbsorbedMember `json:"absorbed_members,omitempty"`
	// OriginChanged is set when the fold changed the survivor's provenance or source speaker
	// (an external member makes the survivor external; a speaker survives only when every member
	// agrees). PriorProvenance and PriorSourceSpeaker are the values before, and FoldedProvenance
	// the provenance the fold set. Undo restores them only while the survivor still holds
	// FoldedProvenance, so a provenance the user set afterwards wins.
	OriginChanged      bool    `json:"origin_changed,omitempty"`
	PriorProvenance    string  `json:"prior_provenance,omitempty"`
	PriorSourceSpeaker *string `json:"prior_source_speaker,omitempty"`
	FoldedProvenance   string  `json:"folded_provenance,omitempty"`
}

// UnmarshalJSON accepts confidence buckets written before confidence became numeric.
func (s *MemoryMergeUndoSnapshot) UnmarshalJSON(data []byte) error {
	// alias drops the UnmarshalJSON method so the plain fields decode normally; prior_confidence
	// is shadowed by the outer RawMessage and decoded separately for its legacy bucket strings.
	type alias MemoryMergeUndoSnapshot
	var raw struct {
		alias
		PriorConfidence json.RawMessage `json:"prior_confidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	confidence, err := decodeSnapshotConfidence(raw.PriorConfidence)
	if err != nil {
		return fmt.Errorf("decode prior_confidence: %w", err)
	}
	*s = MemoryMergeUndoSnapshot(raw.alias)
	s.PriorConfidence = confidence
	return nil
}

func decodeSnapshotConfidence(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}

	var confidence float64
	if err := json.Unmarshal(raw, &confidence); err == nil {
		return confidence, nil
	}

	var legacy string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return 0, fmt.Errorf("expected number or legacy confidence bucket")
	}
	switch legacy {
	case "low":
		return 0.3, nil
	case "medium":
		return 0.6, nil
	case "high":
		return 0.9, nil
	}

	confidence, err := strconv.ParseFloat(legacy, 64)
	if err != nil {
		return 0, fmt.Errorf("unsupported confidence %q", legacy)
	}
	return confidence, nil
}

// MemoryMergeEvent is an append-only log row for memory dedupe/merge actions.
type MemoryMergeEvent struct {
	ent.Schema
}

func (MemoryMergeEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("user_id", uuid.UUID{}).
			Comment("Owner of the affected memories"),
		field.UUID("survivor_memory_id", uuid.UUID{}).
			Comment("Memory row that remains active after the merge. For link events this is the anchor (first) member; no rows are retired."),
		field.Enum("merge_type").
			Values("create", "fold_live", "link").
			Comment("create = new row; fold_live = updated an existing live memory in place; link = cross-referenced related-but-distinct memories (no fold, all surfaces kept)"),
		field.UUID("link_group_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("For link events: the link_group_id assigned to the member memories, so the link can be reverted"),
		field.Text("content").
			Comment("Survivor content after the merge (the canonical content when a fold rewrote the survivor)"),
		field.Int("duplicates_folded").
			Default(1).
			Comment("How many extracted duplicates were folded into this event"),
		field.JSON("source_members", []MemoryMergeSourceMember{}).
			Optional().
			Comment("Pre-merge candidate snapshot for audit/review"),
		field.JSON("snapshot", &MemoryMergeUndoSnapshot{}).
			Optional().
			Comment("Prior survivor state (content, embedding, confidence, chain metadata) and absorbed members for fold_live undo"),
		field.Time("reverted_at").
			Optional().
			Nillable().
			Comment("When this merge was undone, if applicable"),
		field.UUID("compaction_event_id", uuid.UUID{}).
			Optional().
			Nillable().
			Comment("The compaction (checkpoint) that produced this merge/link, if any; new memory creates use CompactionEvent.created_memories instead"),
	}
}

// Edges wires each merge event back to the compaction that produced it, so a CompactionEvent can
// enumerate every merge/link/create it triggered. Nullable: legacy events (and any created outside a
// compaction) carry no compaction_event_id.
func (MemoryMergeEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("compaction_event", CompactionEvent.Type).
			Ref("merge_events").
			Field("compaction_event_id").
			Unique(),
	}
}

func (MemoryMergeEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
		// Recall's lifecycle_events target filter scopes by user, survivor, and then presents
		// newest-first, so keep that interactive audit query index-backed.
		index.Fields("user_id", "survivor_memory_id", "created_at"),
		index.Fields("compaction_event_id"),
	}
}

func (MemoryMergeEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}
