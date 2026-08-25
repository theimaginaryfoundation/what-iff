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
)

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
	RevertedAt        *time.Time                `json:"reverted_at,omitempty"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}
