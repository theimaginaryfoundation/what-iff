package search

import (
	"context"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Store defines the datastore methods the cross-resource search handler needs.
// Each method is owner-scoped at the datastore layer; the handler only ever
// passes the authenticated user's ID through. The interface is intentionally
// narrow so handler tests can stub each section independently.
type Store interface {
	ListChats(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error)
	ListPersonalities(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error)
	ListRituals(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.RitualFilters) (*models.PaginatedResponse, error)
	ListMemories(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MemoryFilters) (*models.PaginatedResponse, error)
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)

	// GetLatestMessagesForChats batch-loads the most recent message body per
	// chat so the handler can enrich chat hits with a snippet without N round
	// trips on the response path.
	GetLatestMessagesForChats(ctx context.Context, userID uuid.UUID, chatIDs []uuid.UUID) (map[uuid.UUID]string, error)
}
