package webhook

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type Provider interface {
	CreateWebhookToken(ctx context.Context, userID uuid.UUID, name string, scopes []models.WebhookScope) (*models.WebhookToken, string, error)
	ListWebhookTokens(ctx context.Context, userID uuid.UUID) ([]*models.WebhookToken, error)
	RevokeWebhookToken(ctx context.Context, userID, tokenID uuid.UUID) error
	CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error)

	// Reads, behind the chat:read scope. These are the same datastore calls the session handlers
	// use, so ownership is enforced in the query: a record that belongs to another account is
	// indistinguishable from one that does not exist.
	ListChats(ctx context.Context, userID uuid.UUID, page, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error)
	ListPersonalities(ctx context.Context, userID uuid.UUID, page, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error)
	ListChatMessages(ctx context.Context, userID, chatID uuid.UUID, page, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error)
	ListChatMessagesBefore(ctx context.Context, userID, chatID uuid.UUID, beforeSentAt time.Time, beforeID uuid.UUID, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error)
	GetChatMessage(ctx context.Context, userID, messageID uuid.UUID) (*models.ChatMessage, error)
	GetJob(ctx context.Context, userID, jobID uuid.UUID) (*models.Job, error)
}

type MessageAgent interface {
	HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error)
	HandleAgentJobPromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error)
	HandleAgentJobPrompt(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID, ritualIDs []uuid.UUID, trackingJob *models.Job) (*models.ChatMessage, error)
}
