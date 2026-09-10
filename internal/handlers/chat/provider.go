package chat

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Store defines the datastore operations required by the chat handlers.
// Keeping this as an interface makes the handler unit-testable without a DB.
type Store interface {
	CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	ListChats(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error)
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	GetChatContext(ctx context.Context, userID, id uuid.UUID) (*models.ChatContext, error)
	UpdatePersonalityScratchpad(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	UpdateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	DeleteChat(ctx context.Context, userID, id uuid.UUID) error

	// Related resources used by chat endpoints.
	ListChatMessages(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error)
	GetChatMessage(ctx context.Context, userID, messageID uuid.UUID) (*models.ChatMessage, error)
	MarkChatMessagesRead(ctx context.Context, userID, chatID uuid.UUID) (int, error)
	SetChatMessageBookmarked(ctx context.Context, userID, messageID uuid.UUID, bookmarked bool) (*models.ChatMessage, error)
	ListChatMessageBookmarks(ctx context.Context, userID, chatID uuid.UUID) ([]*models.ChatMessage, error)
	CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error)
	DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error
	SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error
	GetAvailableRituals(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.RitualFilters) (*models.PaginatedResponse, error)
	GetSystemBindingsForUser(ctx context.Context, userID uuid.UUID) ([]*models.SystemRitualBinding, error)
	ListChatMCPServers(ctx context.Context, userID, chatID uuid.UUID) ([]*models.MCPServer, error)
	ListAvailableChatMCPServers(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.MCPServerFilters) (*models.PaginatedResponse, error)
	AddMCPServerToChat(ctx context.Context, userID, chatID, mcpServerID uuid.UUID) error
	RemoveMCPServerFromChat(ctx context.Context, userID, chatID, mcpServerID uuid.UUID) error
	ListDefaultEnabledMCPServers(ctx context.Context, userID uuid.UUID) ([]*models.MCPServer, error)
	ExportChat(ctx context.Context, userID, chatID uuid.UUID, w io.Writer) error
	ImportChats(ctx context.Context, userID uuid.UUID, convs []models.ImportConversation, onProgress func(imported, skipped int)) (*models.ImportResult, error)

	// Background-job helpers for async conversation import.
	CreateJob(ctx context.Context, userID uuid.UUID, jobModel models.Job) (*models.Job, error)
	UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error)
	UpdateJobProgress(ctx context.Context, userID, id uuid.UUID, progress string) error

	GetModelByName(ctx context.Context, name string) (*models.Model, error)
	IsFirstChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error)
	CountAllChatMessages(ctx context.Context, userID uuid.UUID, cap int) (int, error)

	// GetUserByID returns the user for timezone and other profile data (e.g. when building agent context).
	GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error)

	FindLatestActiveChatMessageJob(ctx context.Context, userID, userMessageID uuid.UUID) (*models.Job, error)
}
