package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

var errNotImplemented = errors.New("not implemented")

func (m *mockProvider) ListChats(ctx context.Context, userID uuid.UUID, page, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
	if m.listChatsFn != nil {
		return m.listChatsFn(ctx, userID, page, pageSize, filters)
	}
	return nil, errNotImplemented
}

func (m *mockProvider) ListPersonalities(ctx context.Context, userID uuid.UUID, page, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
	if m.listPersonalitiesFn != nil {
		return m.listPersonalitiesFn(ctx, userID, page, pageSize, filters)
	}
	return nil, errNotImplemented
}

func (m *mockProvider) ListChatMessages(ctx context.Context, userID, chatID uuid.UUID, page, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	if m.listMessagesFn != nil {
		return m.listMessagesFn(ctx, userID, chatID, page, pageSize, filters)
	}
	return nil, errNotImplemented
}

func (m *mockProvider) ListChatMessagesBefore(ctx context.Context, userID, chatID uuid.UUID, beforeSentAt time.Time, beforeID uuid.UUID, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	if m.listMessagesBeforeFn != nil {
		return m.listMessagesBeforeFn(ctx, userID, chatID, beforeSentAt, beforeID, pageSize, filters)
	}
	return nil, errNotImplemented
}

func (m *mockProvider) GetChatMessage(ctx context.Context, userID, messageID uuid.UUID) (*models.ChatMessage, error) {
	if m.getMessageFn != nil {
		return m.getMessageFn(ctx, userID, messageID)
	}
	return nil, errNotImplemented
}

func (m *mockProvider) GetJob(ctx context.Context, userID, jobID uuid.UUID) (*models.Job, error) {
	if m.getJobFn != nil {
		return m.getJobFn(ctx, userID, jobID)
	}
	return nil, errNotImplemented
}
