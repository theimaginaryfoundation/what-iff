package models

import (
	"time"

	"github.com/google/uuid"
)

// WebhookTokenStatus defines the lifecycle state of a webhook token.
type WebhookTokenStatus string

const (
	WebhookTokenStatusActive  WebhookTokenStatus = "active"
	WebhookTokenStatusRevoked WebhookTokenStatus = "revoked"
)

// WebhookToken is a user-scoped API token used for webhook-authenticated routes.
type WebhookToken struct {
	ID         uuid.UUID          `json:"id"`
	UserID     uuid.UUID          `json:"user_id"`
	Name       string             `json:"name"`
	Status     WebhookTokenStatus `json:"status"`
	LastUsedAt *time.Time         `json:"last_used_at,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

// WebhookAuthPrincipal captures webhook authentication identity details.
type WebhookAuthPrincipal struct {
	UserID         uuid.UUID
	Role           string
	Timezone       string
	WebhookTokenID uuid.UUID
}
