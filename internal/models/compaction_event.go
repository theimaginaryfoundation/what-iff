package models

import (
	"time"

	"github.com/google/uuid"
)

// CheckpointSnapshotKind distinguishes a conversation-summary snapshot from a scratchpad snapshot.
type CheckpointSnapshotKind string

const (
	CheckpointSnapshotKindSummary    CheckpointSnapshotKind = "summary"
	CheckpointSnapshotKindScratchpad CheckpointSnapshotKind = "scratchpad"
)

// CheckpointSnapshot is a content-addressed capture of a summary or scratchpad state. The same row is
// referenced as the "new" state of one compaction and the "old" state of the next.
type CheckpointSnapshot struct {
	ID            uuid.UUID              `json:"id"`
	Kind          CheckpointSnapshotKind `json:"kind"`
	ChatID        *uuid.UUID             `json:"chat_id,omitempty"`
	PersonalityID *uuid.UUID             `json:"personality_id,omitempty"`
	Content       string                 `json:"content"`
	CreatedAt     time.Time              `json:"created_at"`
}

// CompactionLoadedMemory is one memory that was live in the compacted segment (prefetch + search).
type CompactionLoadedMemory struct {
	MemoryID   *uuid.UUID `json:"memory_id,omitempty"`
	Content    string     `json:"content"`
	Scope      string     `json:"scope"`
	Confidence float64    `json:"confidence,omitempty"`
}

// CompactionEvent is the audit record for one conversation checkpoint. It captures the before/after
// of both the conversation summary and the personality scratchpad, the loaded-memory set, memories
// newly created during the compaction, and merge/link actions that changed existing agent state.
// Explanation fields describe the transition and therefore live on the event rather than on the
// content-addressed snapshots, which can be reused by adjacent events.
type CompactionEvent struct {
	ID                    uuid.UUID  `json:"id"`
	ChatID                uuid.UUID  `json:"chat_id"`
	ChatName              string     `json:"chat_name,omitempty"`
	PersonalityID         *uuid.UUID `json:"personality_id,omitempty"`
	AssistantMessageID    *uuid.UUID `json:"assistant_message_id,omitempty"`
	Provider              string     `json:"provider,omitempty"`
	Reason                string     `json:"reason,omitempty"`
	SummaryExplanation    string     `json:"summary_explanation,omitempty"`
	ScratchpadExplanation string     `json:"scratchpad_explanation,omitempty"`

	OldSummary    *CheckpointSnapshot `json:"old_summary,omitempty"`
	NewSummary    *CheckpointSnapshot `json:"new_summary,omitempty"`
	OldScratchpad *CheckpointSnapshot `json:"old_scratchpad,omitempty"`
	NewScratchpad *CheckpointSnapshot `json:"new_scratchpad,omitempty"`

	LoadedMemories  []CompactionLoadedMemory `json:"loaded_memories,omitempty"`
	CreatedMemories []CompactionLoadedMemory `json:"created_memories,omitempty"`
	MergeEvents     []MemoryMergeEvent       `json:"merge_events,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CompactionEventInput carries everything known when a compaction begins (before the new summary is
// produced). NewSummary and its explanation are filled in once summarization completes.
type CompactionEventInput struct {
	ChatID                uuid.UUID
	PersonalityID         *uuid.UUID
	AssistantMessageID    *uuid.UUID
	Provider              string
	Reason                string
	OldSummary            string
	OldScratchpad         string
	NewScratchpad         string
	ScratchpadExplanation string
	// HasScratchpad is false when the checkpoint had no personality (so no scratchpad snapshots are
	// recorded). It disambiguates "scratchpad was genuinely empty" from "no scratchpad step ran".
	HasScratchpad  bool
	LoadedMemories []CompactionLoadedMemory
}
