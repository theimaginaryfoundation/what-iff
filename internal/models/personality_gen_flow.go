package models

import (
	"time"

	"github.com/google/uuid"
)

// ImageStyleNone is the canonical style value that disables all image generation
// for a personality. When set, no portrait or expression grid is generated and
// the image panel is hidden in the UI.
const ImageStyleNone = "none"

// PersonalityGenFlow represents an in-progress or completed personality generation wizard session.
type PersonalityGenFlow struct {
	ID               uuid.UUID         `json:"id"`
	Status           string            `json:"status"`
	CurrentStep      int               `json:"current_step"`
	Answers          map[string]string `json:"answers"`
	GeneratedPrompt  string            `json:"generated_prompt"`
	GeneratedAboutMe string            `json:"generated_about_me"`
	GeneratedNames   []string          `json:"generated_names"`
	PersonalityID    *uuid.UUID        `json:"personality_id,omitempty"`
	// ImageStyle is the style hint chosen on page 1 (e.g. "auto", "anime", "none").
	ImageStyle string `json:"image_style"`
	// ReferenceImageID is the gallery attachment the user uploaded as a visual reference.
	// When set, it becomes the personality cover image on accept instead of a generated portrait.
	ReferenceImageID  *uuid.UUID `json:"reference_image_id,omitempty"`
	ReferenceImageURL *string    `json:"reference_image_url,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// UpdateFlowRequest is the payload for saving partial wizard progress.
type UpdateFlowRequest struct {
	CurrentStep      int               `json:"current_step"`
	Answers          map[string]string `json:"answers"`
	ImageStyle       string            `json:"image_style"`
	ReferenceImageID *uuid.UUID        `json:"reference_image_id,omitempty"`
}

// AcceptFlowRequest is the payload for accepting a generated personality.
type AcceptFlowRequest struct {
	Name         string     `json:"name"`
	CoverImageID *uuid.UUID `json:"cover_image_id,omitempty"`
}
