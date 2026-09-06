package models

import (
	"time"

	"github.com/google/uuid"
)

// MCPServer represents a user-configured remote MCP server.
// AuthToken is intentionally excluded from JSON responses.
type MCPServer struct {
	ID                         uuid.UUID   `json:"id"`
	UserID                     uuid.UUID   `json:"user_id"`
	Name                       string      `json:"name"`
	Description                string      `json:"description"`
	ServerURL                  string      `json:"server_url"`
	AuthMode                   string      `json:"auth_mode"`
	AuthToken                  string      `json:"-"`
	OAuthAuthURL               string      `json:"oauth_auth_url,omitempty"`
	OAuthTokenURL              string      `json:"oauth_token_url,omitempty"`
	OAuthClientID              string      `json:"oauth_client_id,omitempty"`
	OAuthScopes                []string    `json:"oauth_scopes,omitempty"`
	OAuthPKCEPolicy            string      `json:"oauth_pkce_policy,omitempty"`
	OAuthClientSecret          string      `json:"-"`
	OAuthAccessToken           string      `json:"-"`
	OAuthRefreshToken          string      `json:"-"`
	OAuthAccessTokenExpiresAt  *time.Time  `json:"oauth_access_token_expires_at,omitempty"`
	OAuthRefreshTokenExpiresAt *time.Time  `json:"oauth_refresh_token_expires_at,omitempty"`
	OAuthAuthenticatedAt       *time.Time  `json:"oauth_authenticated_at,omitempty"`
	OAuthLastRefreshAt         *time.Time  `json:"oauth_last_refresh_at,omitempty"`
	OAuthRefreshFailCount      int         `json:"oauth_refresh_fail_count,omitempty"`
	OAuthHasRefreshToken       bool        `json:"oauth_has_refresh_token,omitempty"`
	OAuthHasAccessToken        bool        `json:"oauth_has_access_token,omitempty"`
	Status                     string      `json:"status"`
	StatusReason               string      `json:"status_reason,omitempty"`
	ErrorMessage               string      `json:"error_message,omitempty"`
	DefaultEnabled             bool        `json:"default_enabled"`
	RitualIDs                  []uuid.UUID `json:"ritual_ids,omitempty"`
	LastCheckedAt              *time.Time  `json:"last_checked_at,omitempty"`
	LastHealthyAt              *time.Time  `json:"last_healthy_at,omitempty"`
	ToolCount                  int         `json:"tool_count,omitempty"`
	CreatedAt                  time.Time   `json:"created_at"`
	UpdatedAt                  time.Time   `json:"updated_at"`
}

const (
	MCPServerAuthModeHeader = "header"
	MCPServerAuthModeOAuth  = "oauth"
)

const (
	MCPServerPKCERequired     = "required"
	MCPServerPKCESupported    = "supported"
	MCPServerPKCENotSupported = "not_supported"
)

const (
	MCPServerStatusActive       = "active"
	MCPServerStatusExpiring     = "expiring"
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

// MCPOAuthSecretUpdate controls tri-state updates for encrypted OAuth client secret.
// - Provided=false: no change
// - Provided=true, Clear=true: clear existing secret
// - Provided=true, Clear=false, Value non-empty: set new value
type MCPOAuthSecretUpdate struct {
	Provided bool
	Clear    bool
	Value    string
}

type MCPOAuthTokenSet struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  *time.Time
	RefreshTokenExpiresAt *time.Time
	AuthenticatedAt       *time.Time
	LastRefreshAt         *time.Time
}
