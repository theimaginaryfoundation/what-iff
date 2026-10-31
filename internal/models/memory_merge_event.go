package models

import (
	"time"

	"github.com/google/uuid"
)

type MemoryMergeType string

const (
	MemoryMergeTypeCreate   MemoryMergeType = "create"
	MemoryMergeTypeFoldLive MemoryMergeType = "fold_live"
	// MemoryMergeTypeLink cross-references related-but-distinct memories that share a
	// link_group_id. No rows are folded or de-indexed; every surface is preserved.
	MemoryMergeTypeLink MemoryMergeType = "link"
	// MemoryMergeTypeRemove records an agent removing one memory from active memory with
	// remove_memory (ADR 0x036). The memory row is kept, inactive; undo restores it.
	MemoryMergeTypeRemove MemoryMergeType = "remove"
)

// MaxMemoryRemovalReasonRunes bounds the reason an agent gives for removing a memory.
const MaxMemoryRemovalReasonRunes = 500

// MemoryRemoval is who removed a memory, from which conversation, and why. PersonalityID is nil
// for a conversation without a personality.
type MemoryRemoval struct {
	PersonalityID *uuid.UUID `json:"personality_id,omitempty"`
	ChatID        *uuid.UUID `json:"chat_id,omitempty"`
	Reason        string     `json:"reason,omitempty"`
}

type MemoryMergeUndoSnapshot struct {
	PriorConfidence          float64              `json:"prior_confidence,omitempty"`
	PriorChainMetadata       *MemoryChainMetadata `json:"prior_chain_metadata,omitempty"`
	PriorChainMetadataWasNil bool                 `json:"prior_chain_metadata_was_nil"`
}

type MemoryMergeSourceMember struct {
	Content    string           `json:"content"`
	Scope      string           `json:"scope"`
	Confidence MemoryConfidence `json:"confidence,omitempty"`
	MemoryID   *uuid.UUID       `json:"memory_id,omitempty"`
	IsNew      bool             `json:"is_new"`
}

// MemoryMergeEventFilters narrows ListMemoryMergeEvents. All fields are optional; a zero value
// applies no filtering beyond the caller's userID.
type MemoryMergeEventFilters struct {
	Query            *string
	SurvivorMemoryID *uuid.UUID
	MinDate, MaxDate *time.Time
	// ExcludeReverted, when true, restricts results to events with RevertedAt == nil.
	ExcludeReverted bool
	// OnlyChatID, when set, restricts results to fold events whose survivor memory was created in
	// that chat (link events are excluded: their members can come from anywhere). It is the
	// sandbox gate for a chat reading memory history and is never taken from a request.
	OnlyChatID *uuid.UUID
}

type MemoryMergeEvent struct {
	ID               uuid.UUID       `json:"id"`
	SurvivorMemoryID uuid.UUID       `json:"survivor_memory_id"`
	MergeType        MemoryMergeType `json:"merge_type"`
	Content          string          `json:"content"`
	DuplicatesFolded int             `json:"duplicates_folded"`
	// LinkGroupID is set for link events: the shared id assigned to the cross-referenced members.
	LinkGroupID *uuid.UUID `json:"link_group_id,omitempty"`
	// CompactionEventID groups this event under the compaction (checkpoint) that produced it.
	CompactionEventID *uuid.UUID                `json:"compaction_event_id,omitempty"`
	SourceMembers     []MemoryMergeSourceMember `json:"source_members,omitempty"`
	Snapshot          *MemoryMergeUndoSnapshot  `json:"snapshot,omitempty"`
	// Removal is set on a remove event.
	Removal    *MemoryRemoval `json:"removal,omitempty"`
	RevertedAt *time.Time     `json:"reverted_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}
