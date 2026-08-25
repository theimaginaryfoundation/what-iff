package mcpserver

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type Provider interface {
	CreateMCPServer(ctx context.Context, userID uuid.UUID, server models.MCPServer) (*models.MCPServer, error)
	GetMCPServer(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error)
	ListMCPServers(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MCPServerFilters) (*models.PaginatedResponse, error)
	UpdateMCPServer(ctx context.Context, userID uuid.UUID, server models.MCPServer, authTokenUpdate models.MCPServerAuthTokenUpdate, ritualIDsUpdate *[]uuid.UUID) (*models.MCPServer, error)
	DeleteMCPServer(ctx context.Context, userID, id uuid.UUID) error
}
