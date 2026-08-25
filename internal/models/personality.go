package models

import (
	"time"

	"github.com/google/uuid"
)

type Personality struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	SystemPrompt      string    `json:"system_prompt"`
	Scratchpad        string    `json:"scratchpad"`
	ScratchpadHistory []string  `json:"scratchpad_history"`
	// Deprecated: persisted for compatibility; ignored by archival/memory paths.
	ArchivalModel string `json:"archival_model"`
	// Optional override for checkpoint scratchpad update; empty uses the agent default prompt.
	ScratchpadUpdatePrompt string `json:"scratchpad_update_prompt"`
	// Deprecated: persisted for compatibility; ignored by archival/memory paths.
	MemorySearchPrompt string `json:"memory_search_prompt"`
	// Deprecated: persisted for compatibility; ignored by archival/memory paths.
	MemoryWritePrompt string           `json:"memory_write_prompt"`
	AutoPinMemories   bool             `json:"auto_pin_memories"`
	FileAttachments   []FileAttachment `json:"file_attachments"`
	// MoodIDs lists all moods attached to this personality as available moods.
	MoodIDs []uuid.UUID `json:"mood_ids,omitempty"`
	// CoverImageID, when present, references a user-owned image used as this personality's portrait.
	// On create/update, set this to a gallery image ID owned by the user. Send null to clear.
	CoverImageID *uuid.UUID `json:"cover_image_id"`
	// CoverImageURL is a server-derived URL for the cover image (read-only).
	CoverImageURL *string `json:"cover_image_url"`
	// AccentColor is an optional user-selected hex color used for persona UI accenting.
	AccentColor *string `json:"accent_color,omitempty"`
	// ThumbnailCircle stores normalized portrait framing for circular thumbnails.
	ThumbnailCircle *PersonalityThumbnailCircle `json:"thumbnail_circle,omitempty"`
	// ExpressionsEnabled controls whether expression picking runs in chat and
	// expression frames are rendered in the UI. Defaults to true.
	ExpressionsEnabled bool `json:"expressions_enabled"`
	// ImageStyle is the preferred image generation style (e.g. "auto", "anime", "none").
	// Persisted from the generation wizard and used for future expression grid generations.
	ImageStyle string                `json:"image_style"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
	Stats      PersonalityUsageStats `json:"stats"`
}

type PersonalityThumbnailCircle struct {
	CX float64 `json:"cx"`
	CY float64 `json:"cy"`
	R  float64 `json:"r"`
}

type PersonalityUsageStats struct {
	ChatCount  int        `json:"chat_count"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type PersonalityExpression struct {
	ID            uuid.UUID  `json:"id"`
	ExpressionKey string     `json:"expression_key"`
	Label         *string    `json:"label"`
	ImageID       *uuid.UUID `json:"image_id"`
	ImageURL      *string    `json:"image_url"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type UpdatePersonalityExpressionRequest struct {
	ImageID  *uuid.UUID `json:"image_id,omitempty"`
	ImageSet bool       `json:"-"`
	Label    *string    `json:"label,omitempty"`
	LabelSet bool       `json:"-"`
}

type PersonalityFilters struct {
	Name    *string     `json:"name,omitempty"`
	Query   *string     `json:"query,omitempty"`
	MinDate *time.Time  `json:"min_date,omitempty"`
	MaxDate *time.Time  `json:"max_date,omitempty"`
	IDs     []uuid.UUID `json:"personality_ids,omitempty"`
}
