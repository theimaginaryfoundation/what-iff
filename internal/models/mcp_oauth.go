package models

import (
	"time"

	"github.com/google/uuid"
)

type MCPOAuthSession struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	MCPServerID   uuid.UUID
	State         string
	CodeVerifier  string
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	RedirectAfter string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
