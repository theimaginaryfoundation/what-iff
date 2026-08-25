package webhook

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type Provider interface {
	CreateWebhookToken(ctx context.Context, userID uuid.UUID, name string) (*models.WebhookToken, string, error)
	ListWebhookTokens(ctx context.Context, userID uuid.UUID) ([]*models.WebhookToken, error)
	RevokeWebhookToken(ctx context.Context, userID, tokenID uuid.UUID) error
	CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error)
}

type MessageAgent interface {
	HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error)
	HandleAgentJobPromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error)
	HandleAgentJobPrompt(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID, ritualIDs []uuid.UUID, trackingJob *models.Job) (*models.ChatMessage, error)
}
