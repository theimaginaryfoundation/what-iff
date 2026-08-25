package models

import (
	"time"

	"github.com/google/uuid"
)

// SystemRitualBinding represents a per-user binding for a system ritual.
type SystemRitualBinding struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	RitualID  uuid.UUID `json:"ritual_id"`
	Hotkeys   string    `json:"hotkeys"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
