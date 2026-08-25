package models

import (
	"time"

	"github.com/google/uuid"
)

// Mood bundles an image, skills (rituals), a prompt snippet, and a name/description
// into a reusable "mood" for an agent personality.
type Mood struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	PromptSnippet string    `json:"prompt_snippet"`
	// RecommendedModel is an optional model name that becomes active when the mood is selected.
	RecommendedModel string `json:"recommended_model,omitempty"`
	// ImageIDs contains zero or one file attachment UUID attached to this mood.
	ImageIDs []uuid.UUID `json:"image_ids,omitempty"`
	// RitualIDs lists the ritual UUIDs attached to this mood.
	RitualIDs []uuid.UUID `json:"ritual_ids,omitempty"`
	// PersonalityIDs lists the personalities this mood is attached to (populated on GET /mood/{id}).
	PersonalityIDs []uuid.UUID `json:"personality_ids,omitempty"`
	// ThumbnailData is a base64-encoded JPEG thumbnail (up to ~128×128), auto-generated
	// from the attached image. Omitted from list responses; fetch via
	// GET /api/mood/{id} or included inline in the personality response.
	ThumbnailData string    `json:"thumbnail_data,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// MoodFilters defines optional filter criteria for listing moods.
type MoodFilters struct {
	Name    *string    `json:"name,omitempty"`
	MinDate *time.Time `json:"min_date,omitempty"`
	MaxDate *time.Time `json:"max_date,omitempty"`
}

// CreateMoodRequest is the payload for POST /api/mood.
type CreateMoodRequest struct {
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	PromptSnippet string      `json:"prompt_snippet"`
	ImageIDs      []uuid.UUID `json:"image_ids,omitempty"`
	RitualIDs     []uuid.UUID `json:"ritual_ids,omitempty"`
}

// UpdateMoodRequest is the payload for PUT /api/mood/{id}.
type UpdateMoodRequest struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	PromptSnippet    string `json:"prompt_snippet"`
	RecommendedModel string `json:"recommended_model"`
	// When non-nil, replaces the mood's attached image entirely (0 or 1 ID).
	ImageIDs *[]uuid.UUID `json:"image_ids,omitempty"`
	// When non-nil, replaces the mood's attached rituals entirely.
	RitualIDs *[]uuid.UUID `json:"ritual_ids,omitempty"`
}

// AttachMoodToPersonalitiesRequest is the payload for POST /api/mood/{id}/personalities.
// It replaces the full set of personality associations for the mood.
type AttachMoodToPersonalitiesRequest struct {
	PersonalityIDs []uuid.UUID `json:"personality_ids"`
}
