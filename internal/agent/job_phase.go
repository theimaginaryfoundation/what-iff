package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (a *Agent) advanceChatJobStatus(ctx context.Context, chatJob *models.Job, status models.JobStatus) error {
	if chatJob == nil {
		return nil
	}
	toSave := *chatJob
	toSave.Status = status
	updated, err := a.ds.UpdateJob(ctx, chatJob.UserID, toSave)
	if err != nil {
		return fmt.Errorf("update job status %s: %w", status, err)
	}
	*chatJob = *updated
	return nil
}

// persistInferencePhase updates job to inference_complete, persists user-turn tokens when requested,
// and updates chat metadata for continuity. chatJob must be non-nil.
// User-turn token and chat continuity updates are best-effort after the job row is updated: an
// assistant reply already exists, so we avoid failing the whole turn on secondary persistence errors.
func (a *Agent) persistInferencePhase(ctx context.Context, chatJob *models.Job, chatMessage *models.ChatMessage, chat *models.Chat, chatCtx *chatContext, result *provider.GenerateResponse, agentMessage *models.ChatMessage, persistUserTurnUpdate bool) error {
	if chatJob == nil {
		a.logger.Error("persistInferencePhase: chatJob is nil")
		return fmt.Errorf("persistInferencePhase: chatJob is nil")
	}
	if chatMessage == nil {
		return fmt.Errorf("persistInferencePhase: chatMessage is nil")
	}
	if agentMessage == nil {
		return fmt.Errorf("persistInferencePhase: agentMessage is nil")
	}
	toSave := *chatJob
	toSave.Status = models.JobStatusInferenceComplete
	toSave.ResultID = &agentMessage.ID
	updated, err := a.ds.UpdateJob(ctx, chatJob.UserID, toSave)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}
	if err := a.ds.ClearJobDraftDeltas(ctx, chatJob.UserID, chatJob.ID); err != nil {
		return fmt.Errorf("failed to clear job draft deltas: %w", err)
	}
	*chatJob = *updated

	a.persistUserTurnAndChatAfterInference(ctx, chatJob.UserID, chatMessage, chat, chatCtx, result, persistUserTurnUpdate)

	if err := a.ds.SetChatMessageLastError(ctx, chatJob.UserID, chatMessage.ID, nil); err != nil {
		a.logger.Warn("failed to clear user message last_error_message after inference", zap.Error(err))
	}

	return nil
}

// persistUserTurnAndChatAfterInference updates user-turn tokens/context and chat continuity after inference.
// Errors are logged only; callers should have already marked the job inference-complete when required.
func (a *Agent) persistUserTurnAndChatAfterInference(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage, chat *models.Chat, chatCtx *chatContext, result *provider.GenerateResponse, persistUserTurnUpdate bool) {
	if persistUserTurnUpdate && chatMessage != nil && result != nil {
		chatMessage.Tokens = result.InputTokens
		if chatCtx != nil {
			chatMessage.AdditionalContext = additionalContextItemsFromChatContext(chatCtx)
		}
		if _, err := a.ds.UpdateChatMessage(ctx, userID, *chatMessage); err != nil {
			a.logger.Warn("failed to update user message tokens after inference", zap.Error(err))
		}
	}

	if chat != nil && result != nil {
		chat.ResponseID = &result.ID
		lastMessageTime := time.Unix(result.CreatedAt, 0)
		chat.LastMessageTime = &lastMessageTime
		if _, err := a.ds.UpdateChat(ctx, userID, *chat); err != nil {
			a.logger.Warn("failed to update chat continuity after inference", zap.Error(err))
		}
	}
}

// applyExpressionPhase runs expression classification and advances the job when present.
// chatCtx must be non-nil for expression picking; if nil, expression picking is skipped and the job is still advanced when chatJob is non-nil.
func (a *Agent) applyExpressionPhase(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatCtx *chatContext, inferenceModelCtx *provider.ModelContext, userTurn string, agentMessage *models.ChatMessage) error {
	if chatCtx == nil {
		a.logger.Warn("applyExpressionPhase: chatCtx is nil; skipping expression picking")
		if chatJob == nil {
			return nil
		}
		return a.advanceChatJobStatus(ctx, chatJob, models.JobStatusExpressionComplete)
	}
	if !chatCtx.expressionsEnabled {
		// Treat disabled expressions as a clean completion (not an error) so the
		// job advances normally and the client receives its status update.
		if chatJob == nil {
			return nil
		}
		return a.advanceChatJobStatus(ctx, chatJob, models.JobStatusExpressionComplete)
	}
	// Mock/local mode: the expression classifier is a provider call — skip it
	// intentionally and advance the job like the disabled-expressions path.
	if a.nonVendorLLM() {
		a.logger.Debug("mock/local mode: skipping expression classification")
		if chatJob == nil {
			return nil
		}
		return a.advanceChatJobStatus(ctx, chatJob, models.JobStatusExpressionComplete)
	}
	pid := chatCtx.chat.PersonalityID
	var exprID *uuid.UUID
	var reasoning string
	var err error
	if pid != uuid.Nil {
		exprID, reasoning, err = a.PickGenerationExpression(ctx, userID, pid, inferenceModelCtx, userTurn, agentMessage.Message)
		if err != nil {
			a.logger.Warn("expression picker failed", zap.Error(err))
		}
	}
	if exprID != nil {
		if _, uerr := a.ds.UpdateChatMessageGenerationExpression(ctx, userID, agentMessage.ID, exprID, reasoning); uerr != nil {
			a.logger.Warn("failed to persist generation expression", zap.Error(uerr))
		}
	}
	if chatJob == nil {
		return nil
	}
	return a.advanceChatJobStatus(ctx, chatJob, models.JobStatusExpressionComplete)
}

// advanceJobInferenceComplete marks the async jobs row as inference_complete with the assistant message id.
func (a *Agent) advanceJobInferenceComplete(ctx context.Context, chatJob *models.Job, assistantMessageID uuid.UUID) error {
	if chatJob == nil {
		return nil
	}
	toSave := *chatJob
	toSave.Status = models.JobStatusInferenceComplete
	resultID := assistantMessageID
	toSave.ResultID = &resultID
	updated, err := a.ds.UpdateJob(ctx, chatJob.UserID, toSave)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}
	if err := a.ds.ClearJobDraftDeltas(ctx, chatJob.UserID, chatJob.ID); err != nil {
		return fmt.Errorf("failed to clear job draft deltas: %w", err)
	}
	*chatJob = *updated
	return nil
}
