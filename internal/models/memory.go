package models

import (
	"time"

	"github.com/google/uuid"
)

type MemoryLevel string

const (
	MemoryLevelGlobal      MemoryLevel = "global"
	MemoryLevelPersonality MemoryLevel = "personality"
	MemoryLevelThread      MemoryLevel = "thread"
	MemoryLevelSummary     MemoryLevel = "summary"
)

type MemoryType string

const (
	MemoryTypeContext MemoryType = "Context"
)

type MemoryStatus string

const (
	MemoryStatusActive   MemoryStatus = "active"
	MemoryStatusInactive MemoryStatus = "inactive"
)

// MemoryConfidence is the coarse confidence bucket the extraction/merge LLM emits. Buckets
// calibrate far better than raw floats, so the model keeps producing these — but they are stored
// as a float (see Memory.Confidence) for granularity + forward-compat.
type MemoryConfidence string

const (
	MemoryConfidenceLow    MemoryConfidence = "low"
	MemoryConfidenceMedium MemoryConfidence = "medium"
	MemoryConfidenceHigh   MemoryConfidence = "high"
)

// DefaultMemoryConfidence is the stored float anchor for medium/unknown confidence.
const DefaultMemoryConfidence = 0.6

// Float maps a coarse confidence bucket to its stored float anchor in [0,1].
func (c MemoryConfidence) Float() float64 {
	switch c {
	case MemoryConfidenceLow:
		return 0.3
	case MemoryConfidenceHigh:
		return 0.9
	default:
		return DefaultMemoryConfidence
	}
}

// MemoryConfidenceFromFloat buckets a stored float back into a coarse label — used for display and
// for the LLM-facing pipeline, which still reasons in buckets.
func MemoryConfidenceFromFloat(f float64) MemoryConfidence {
	switch {
	case f < 0.45:
		return MemoryConfidenceLow
	case f < 0.75:
		return MemoryConfidenceMedium
	default:
		return MemoryConfidenceHigh
	}
}

// ClampConfidence bounds a confidence float to [0,1].
func ClampConfidence(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

type MemoryChainMetadata struct {
	DuplicateCount          int         `json:"duplicate_count"`
	VerifiedTimestampsFirst []string    `json:"verified_timestamps_first,omitempty"`
	VerifiedTimestampsLast  []string    `json:"verified_timestamps_last,omitempty"`
	MergedFromMemoryIDs     []uuid.UUID `json:"merged_from_memory_ids,omitempty"`
}

type MemorySort string

const (
	MemorySortCreatedDesc MemorySort = "created_desc"
	MemorySortCreatedAsc  MemorySort = "created_asc"
	MemorySortUpdatedDesc MemorySort = "updated_desc"
)

type Memory struct {
	ID       uuid.UUID    `json:"id"`
	ChatID   uuid.UUID    `json:"chat_id,omitempty,omitzero"`
	ChatName string       `json:"chat_name,omitempty"`
	Content  string       `json:"content"`
	Level    MemoryLevel  `json:"level"`
	Type     MemoryType   `json:"type"`
	Status   MemoryStatus `json:"status"`
	// Confidence is a stored float in [0,1]. The extraction/merge LLM emits coarse buckets that
	// map to anchors (see MemoryConfidence.Float); other signals (e.g. the reconfirmation tally)
	// can refine it to finer values over time.
	Confidence          float64              `json:"confidence"`
	Starred             bool                 `json:"starred"`
	Scope               string               `json:"-"`
	ChainMetadata       *MemoryChainMetadata `json:"chain_metadata,omitempty"`
	PinnedPersonalityID *uuid.UUID           `json:"pinned_personality_id,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	// Relevance is a transient, retrieval-time cosine similarity in [0,1] set by the vector search
	// (GetRelatedMemories). It is not persisted; it's nil for memories not loaded via a query.
	Relevance *float64 `json:"relevance,omitempty"`
}

// MemoryFilters defines filters for listing memories
type MemoryFilters struct {
	ChatID               *uuid.UUID    `json:"chat_id,omitempty"`
	Level                *MemoryLevel  `json:"level,omitempty"`
	Type                 *MemoryType   `json:"type,omitempty"`
	Starred              *bool         `json:"starred,omitempty"`
	PinnedPersonalityID  *uuid.UUID    `json:"pinned_personality_id,omitempty"`
	PinnedPersonalityIDs []uuid.UUID   `json:"pinned_personality_ids,omitempty"`
	GlobalOnly           *bool         `json:"global_only,omitempty"`
	Query                *string       `json:"query,omitempty"`
	Sort                 *MemorySort   `json:"sort,omitempty"`
	MinDate              *time.Time    `json:"min_date,omitempty"`
	MaxDate              *time.Time    `json:"max_date,omitempty"`
	Scope                *string       `json:"-"`
	Status               *MemoryStatus `json:"status,omitempty"`
}

type CreateMemoryInput struct {
	Content             string           `json:"content"`
	Level               MemoryLevel      `json:"level"`
	ChatID              *uuid.UUID       `json:"chat_id,omitempty"`
	PinnedPersonalityID *uuid.UUID       `json:"pinned_personality_id,omitempty"`
	Type                MemoryType       `json:"type"`
	Starred             bool             `json:"starred"`
	Confidence          MemoryConfidence `json:"confidence,omitempty"`
}

type BatchCreateMemoryInput struct {
	Items     []CreateMemoryInput `json:"items"`
	AllOrNone bool                `json:"all_or_none"`
}

type MemoryPatch struct {
	Content                *string           `json:"content,omitempty"`
	Level                  *MemoryLevel      `json:"level,omitempty"`
	ChatID                 *uuid.UUID        `json:"chat_id,omitempty"`
	SetChatID              bool              `json:"-"`
	PinnedPersonalityID    *uuid.UUID        `json:"pinned_personality_id,omitempty"`
	SetPinnedPersonalityID bool              `json:"-"`
	Type                   *MemoryType       `json:"type,omitempty"`
	Starred                *bool             `json:"starred,omitempty"`
	Status                 *MemoryStatus     `json:"status,omitempty"`
	Confidence             *MemoryConfidence `json:"confidence,omitempty"`
}

// ChatSummaryBackfillCandidate identifies a legacy checkpoint summary that has
// not yet been copied into its singleton Summary memory.
type ChatSummaryBackfillCandidate struct {
	UserID  uuid.UUID
	ChatID  uuid.UUID
	Summary string
}

// MemoryRecord is a single memory in the export (used inside each section).
// The Chat edge must be preloaded (WithChat) for ChatID and ChatName to populate.
type MemoryRecord struct {
	ID        uuid.UUID  `json:"id"`
	Content   string     `json:"content"`
	ChatID    *uuid.UUID `json:"chat_id,omitempty"`
	ChatName  *string    `json:"chat_name,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// MemoryImportResult reports import execution stats for a ZIP upload.
type MemoryImportResult struct {
	ImportedCount             int `json:"imported_count"`
	DuplicateCount            int `json:"duplicate_count"`
	InvalidRecordCount        int `json:"invalid_record_count"`
	SkippedMissingChat        int `json:"skipped_missing_chat_count"`
	SkippedMissingPersonality int `json:"skipped_missing_personality_count"`
}
