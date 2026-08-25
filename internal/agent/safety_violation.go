package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// handleSafetyViolation converts a provider moderation block into a normal
// assistant turn and persists a moderation event. Contract: callers should
// continue processing only when err == nil. Any non-nil error indicates we
// could not safely complete this recovery path and the job should fail.
func (a *Agent) handleSafetyViolation(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage, chatCtx *chatContext, violation *provider.SafetyViolation) (*models.ChatMessage, *provider.GenerateResponse, error) {
	if violation == nil {
		return nil, nil, fmt.Errorf("safety violation details missing")
	}

	eventInput := models.CreateSafetyViolationEventInput{
		OccurredAt:      time.Now(),
		Provider:        violation.Provider,
		ViolationType:   violation.ViolationType,
		ProviderCode:    violation.ProviderCode,
		ProviderMessage: violation.ProviderMessage,
		RawError:        violation.RawError,
		UserID:          userID,
		ChatID:          &chatMessage.ChatID,
		ChatMessageID:   &chatMessage.ID,
		ChatName:        strings.TrimSpace(chatCtx.chat.Name),
		ChatMessageText: chatMessage.Message,
	}
	if _, err := a.ds.CreateSafetyViolationEvent(ctx, eventInput); err != nil {
		a.logger.Error("failed to persist safety violation event",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatMessage.ChatID.String()),
			zap.Error(err),
		)
	}

	result := &provider.GenerateResponse{
		ID:           "safety_violation_" + uuid.NewString(),
		InputTokens:  0,
		OutputTokens: 0,
		CreatedAt:    time.Now().Unix(),
		Text:         safetyViolationAssistantMessage,
	}

	personalityName := a.resolvePersonalityName(ctx, userID, chatCtx.chat.PersonalityID)
	agentMessage, err := a.saveAgentResponse(ctx, userID, chatMessage.ChatID, result, nil, nil, chatCtx.model, personalityName, activeMoodID(chatCtx.activeMood))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to save safety violation assistant response: %w", err)
	}

	return agentMessage, result, nil
}
