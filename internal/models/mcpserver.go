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
	ErrorMessage   string      `json:"error_message,omitempty"`
	DefaultEnabled bool        `json:"default_enabled"`
	RitualIDs      []uuid.UUID `json:"ritual_ids,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

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
