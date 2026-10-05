package mcpserver

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type Provider interface {
	CreateMCPServer(ctx context.Context, userID uuid.UUID, server models.MCPServer) (*models.MCPServer, error)
	GetMCPServer(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error)
	ListMCPServers(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MCPServerFilters) (*models.PaginatedResponse, error)
	UpdateMCPServer(ctx context.Context, userID uuid.UUID, server models.MCPServer, authTokenUpdate models.MCPServerAuthTokenUpdate, oauthSecretUpdate models.MCPOAuthSecretUpdate, ritualIDsUpdate *[]uuid.UUID) (*models.MCPServer, error)
	UpdateMCPServerRuntimeState(ctx context.Context, userID, mcpServerID uuid.UUID, status, reason string, toolCount int, checkedAt, healthyAt *time.Time) error
	DeleteMCPServer(ctx context.Context, userID, id uuid.UUID) error
}

type OAuthService interface {
	StartAuth(ctx context.Context, userID, connectorID uuid.UUID, redirectAfter string) (string, error)
	HandleCallback(ctx context.Context, state, code, oauthErr, oauthErrDesc string) (redirectURL, message string, success bool, err error)
	// CallbackURL is the redirect_uri sent to OAuth providers. Users register it
	// with the provider, so the UI shows it; it differs per deployment.
	CallbackURL() string
}
