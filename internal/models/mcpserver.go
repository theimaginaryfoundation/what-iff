package models

import (
	"time"

	"github.com/google/uuid"
)

// MCPServer represents a user-configured remote MCP server.
// AuthToken is intentionally excluded from JSON responses.
type MCPServer struct {
	ID             uuid.UUID   `json:"id"`
	UserID         uuid.UUID   `json:"user_id"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	ServerURL      string      `json:"server_url"`
	AuthToken      string      `json:"-"`
	Status         string      `json:"status"`
	StatusReason   string      `json:"status_reason,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	DefaultEnabled bool        `json:"default_enabled"`
	RitualIDs      []uuid.UUID `json:"ritual_ids,omitempty"`
	LastCheckedAt  *time.Time  `json:"last_checked_at,omitempty"`
	LastHealthyAt  *time.Time  `json:"last_healthy_at,omitempty"`
	ToolCount      int         `json:"tool_count,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

const (
	MCPServerStatusActive       = "active"
	MCPServerStatusRefreshError = "refresh_failed"
	MCPServerStatusInvalid      = "invalid"
	MCPServerStatusDisabled     = "disabled"
)

type MCPServerFilters struct {
	Query *string `json:"query,omitempty"`
}

// MCPServerAuthTokenUpdate is a tri-state patch for authentication token updates.
// Semantics:
// - Provided=false: no change
// - Provided=true, Clear=true: clear existing token
// - Provided=true, Clear=false, Value non-empty: set new token value
// - Provided=true, Clear=false, Value empty: no change
type MCPServerAuthTokenUpdate struct {
	Provided bool
	Clear    bool
	Value    string
}
