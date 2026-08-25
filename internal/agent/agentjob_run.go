package agent

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ephemeralPromptOptions struct {
	skipQuotaCheck     bool
	skipUsageRecording bool
}

// HandleAgentJobPromptAsync creates a background job and processes the prompt asynchronously.
// It returns once the handoff to background processing succeeds.
func (a *Agent) HandleAgentJobPromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error) {
	return a.handleEphemeralPromptAsync(ctx, chatID, prompt, modelOverrideID, personalityOverrideID, ephemeralPromptOptions{})
}

// HandleWelcomeMessagePromptAsync enqueues onboarding welcome generation without
// charging quota or recording usage events for the auto-generated turn.
func (a *Agent) HandleWelcomeMessagePromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error) {
	return a.handleEphemeralPromptAsync(ctx, chatID, prompt, modelOverrideID, personalityOverrideID, ephemeralPromptOptions{
		skipQuotaCheck:     true,
		skipUsageRecording: true,
	})
}

func (a *Agent) handleEphemeralPromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID, opts ephemeralPromptOptions) (*models.ChatMessageResponse, error) {
	// IMPORTANT: detach from request cancellation before enqueue + background execution.
	// We only copy required values (user ID, timezone, plan) into a fresh Background context.
	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	userID, _ := middleware.GetUserIDFromContext(detachedCtx)
	if userID == uuid.Nil {
		return nil, errors.New("user ID not found in context")
	}

	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	backgroundJob, err := a.ds.CreateJob(detachedCtx, userID, models.Job{
		JobType:     JobTypeAgentJobRun,
		Reference:   chatID.String(),
		Status:      models.JobStatusPending,
		DraftDeltas: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create background job: %w", err)
	}

	runCtx, cancel := context.WithCancel(detachedCtx)
	a.registerRunningJobCancel(backgroundJob.ID, userID, cancel)
	go func(jobID uuid.UUID, runCtx context.Context) {
		defer cancel()
		defer a.unregisterRunningJobCancel(jobID)
		// Ensure unexpected panics do not leave jobs stuck in processing.
		defer func() {
			if recovered := recover(); recovered != nil {
				panicMessage := fmt.Sprintf("panic: %v", recovered)
				_, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, panicMessage)
				if failErr != nil {
					a.logger.Error("failed to mark async webhook background job failed after panic",
						zap.String("job_id", jobID.String()),
						zap.Error(failErr),
					)
				}
				a.logger.Error("panic recovered in async webhook background processing",
					zap.String("job_id", jobID.String()),
					zap.String("chat_id", chatID.String()),
					zap.Any("panic", recovered),
					zap.ByteString("stack_trace", debug.Stack()),
				)
			}
		}()

		_, statusErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusProcessing, "")
		if statusErr != nil {
			a.logger.Error("failed to mark async webhook background job processing",
				zap.String("job_id", jobID.String()),
				zap.Error(statusErr),
			)
			return
		}
		if clearErr := a.ds.ClearJobDraftDeltas(runCtx, userID, jobID); clearErr != nil {
			a.logger.Warn("failed to clear job draft deltas before async generation",
				zap.String("job_id", jobID.String()),
				zap.Error(clearErr),
			)
		}

		_, runErr := a.handleEphemeralPrompt(
			runCtx,
			chatID,
			prompt,
			modelOverrideID,
			personalityOverrideID,
			nil,
			backgroundJob,
			models.ActionTypeJobRun,
			telemetry.CallPathAgentJob,
			opts,
		)
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				persistCtx, persistCancel := context.WithTimeout(context.Background(), jobTerminalPersistTimeout)
				defer persistCancel()
				_, cancelErr := a.ds.UpdateJobStatus(persistCtx, userID, jobID, models.JobStatusCancelled, "")
				if cancelErr != nil {
					a.logger.Error("failed to mark async webhook background job cancelled",
						zap.String("job_id", jobID.String()),
						zap.Error(cancelErr),
					)
				}
				a.logger.Info("async webhook background processing cancelled",
					zap.String("job_id", jobID.String()),
					zap.String("chat_id", chatID.String()),
				)
				return
			}
			_, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, runErr.Error())
			if failErr != nil {
				a.logger.Error("failed to mark async webhook background job failed",
					zap.String("job_id", jobID.String()),
					zap.Error(failErr),
				)
			}
			a.logger.Error("async webhook background processing failed",
				zap.String("job_id", jobID.String()),
				zap.String("chat_id", chatID.String()),
				zap.Error(runErr),
			)
			return
		}

		// Phases through inference → expression → compaction are finalized inside HandleAgentJobPrompt.
	}(backgroundJob.ID, runCtx)

	return &models.ChatMessageResponse{
		ID:    backgroundJob.ID,
		JobID: backgroundJob.ID.String(),
		Type:  JobTypeAgentJobRun,
	}, nil
}

// HandleAgentJobPrompt runs the agent for a scheduled AgentJob prompt without persisting
// the injected prompt as a ChatMessage. It always persists the assistant response message.
// ritualIDs are the rituals attached to the agent job; their content is appended to the prompt
// and their MCP servers / disabled-tool overrides are applied for the duration of this run.
// When trackingJob is non-nil (e.g. async agent_job_run), job status advances through the same
// inference → expression → compaction phases as user chat jobs.
func (a *Agent) HandleAgentJobPrompt(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID, ritualIDs []uuid.UUID, trackingJob *models.Job) (*models.ChatMessage, error) {
	return a.handleEphemeralPrompt(ctx, chatID, prompt, modelOverrideID, personalityOverrideID, ritualIDs, trackingJob, models.ActionTypeJobRun, telemetry.CallPathAgentJob, ephemeralPromptOptions{})
}

// HandleEphemeralPromptSync runs a synchronous autonomous prompt against a chat
// without persisting the injected user prompt. Only the assistant response is saved.
func (a *Agent) HandleEphemeralPromptSync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessage, error) {
	return a.handleEphemeralPrompt(ctx, chatID, prompt, modelOverrideID, personalityOverrideID, nil, nil, models.ActionTypeChatMessage, telemetry.CallPathUserChat, ephemeralPromptOptions{})
}

func (a *Agent) handleEphemeralPrompt(
	ctx context.Context,
	chatID uuid.UUID,
	prompt string,
	modelOverrideID *uuid.UUID,
	personalityOverrideID *uuid.UUID,
	ritualIDs []uuid.UUID,
	trackingJob *models.Job,
	actionType string,
	callPath telemetry.CallPath,
	opts ephemeralPromptOptions,
) (*models.ChatMessage, error) {
	ctx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	userID, _ := middleware.GetUserIDFromContext(ctx)
	if userID == uuid.Nil {
		return nil, errors.New("user ID not found in context")
	}

	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	assertNoTestHooksInProduction(a)
	ctx = a.withCallPath(ctx, callPath)

	// Load rituals so their content and tool config can be applied below.
	var jobRituals []*models.Ritual
	if len(ritualIDs) > 0 {
		loaded, err := a.ds.GetRitualsByIDs(ctx, userID, ritualIDs)
		if err != nil {
			a.logger.Warn("failed to load rituals for agent job run; continuing without skills",
				zap.Error(err),
				zap.String("user_id", userID.String()),
				zap.String("chat_id", chatID.String()),
				zap.Int("ritual_id_count", len(ritualIDs)),
			)
		} else {
			jobRituals = loaded
		}
	}

	// Construct an ephemeral user message to carry prompt content into the agent loop,
	// without persisting it to the database.
	ephemeralUserMessage := &models.ChatMessage{
		ID:     uuid.New(),
		ChatID: chatID,
		// Note: this message is never stored, so its exact phrasing does not affect UX.
		Message: prompt,
		Origin:  models.MessageOriginUser,
		SentAt:  time.Now(),
		// Attach rituals so EnrichUserMessageWithRituals and getChatMCPTools run for this turn.
		Rituals: jobRituals,
	}

	// Rehydration gate: stall if this thread's import summary is still in flight (no-op otherwise).
	a.WaitForThreadRehydration(ctx, userID, chatID)

	chatCtx, err := a.prepareChatContext(ctx, userID, ephemeralUserMessage)
	if err != nil {
		return nil, err
	}

	// Values stored on the Chat row — overrides apply only to this run; must not be written back.
	persistedModelID := chatCtx.chat.ModelID
	persistedPersonalityID := chatCtx.chat.PersonalityID

	if modelOverrideID != nil {
		model, err := a.ds.GetModel(ctx, *modelOverrideID)
		if err == nil && model != nil {
			chatCtx.chat.ModelID = model.ID
			chatCtx.chat.ModelName = model.Name
			chatCtx.chat.ToolsEnabled = model.ToolSupport
			chatCtx.model = model.Name
			chatCtx.modelProvider = model.Provider
			chatCtx.modelSubscriptionTier = model.SubscriptionTier
		} else if err != nil && !errors.Is(err, datastore.ErrModelNotFound) {
			return nil, fmt.Errorf("failed to resolve model override: %w", err)
		}
	}
	if personalityOverrideID != nil {
		personality, err := a.ds.GetPersonality(ctx, userID, *personalityOverrideID)
		if err == nil && personality != nil {
			chatCtx.chat.PersonalityID = personality.ID
			chatCtx.chat.SystemPrompt = personality.SystemPrompt
			chatCtx.chat.Scratchpad = personality.Scratchpad
		}
	}

	// Quota gate — checked after overrides are resolved so we use the effective model tier.
	// actionType controls whether free-tier chat eligibility can apply.
	qd := metering.Decision{Allowed: true}
	if !opts.skipQuotaCheck {
		qd = a.meter.Check(ctx, userID, chatCtx.modelSubscriptionTier, actionType)
		if !qd.Allowed {
			return nil, fmt.Errorf("%w for user %s", ErrQuotaExceeded, userID)
		}
	}

	modelContext, err := a.buildModelContextForChatMessage(ctx, userID, ephemeralUserMessage, chatCtx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build model context: %w", err)
	}

	agentMessage, resp, err := a.generateAssistantForMessage(ctx, userID, trackingJob, ephemeralUserMessage, chatCtx, modelContext)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("unexpected nil response")
	}
	ephemeralUserMessage.Tokens = resp.InputTokens

	// Update chat metadata for thread continuity + sorting.
	chatCtx.chat.ResponseID = &resp.ID
	lastMessageTime := time.Unix(resp.CreatedAt, 0)
	chatCtx.chat.LastMessageTime = &lastMessageTime

	// Persist only thread metadata; keep the chat's saved model/personality (do not store job overrides).
	chatForDB := *chatCtx.chat
	chatForDB.ModelID = persistedModelID
	chatForDB.PersonalityID = persistedPersonalityID
	if _, err := a.ds.UpdateChat(ctx, userID, chatForDB); err != nil {
		return nil, fmt.Errorf("failed to update chat metadata: %w", err)
	}

	if err := a.advanceJobInferenceComplete(ctx, trackingJob, agentMessage.ID); err != nil {
		return nil, fmt.Errorf("failed to advance agent job after inference: %w", err)
	}

	exprErr := a.applyExpressionPhase(ctx, userID, trackingJob, chatCtx, modelContext, prompt, agentMessage)

	a.postMessageProcessing(ctx, userID, ephemeralUserMessage, agentMessage, chatCtx, modelContext, actionType, qd, opts.skipUsageRecording)

	if exprErr != nil {
		return nil, fmt.Errorf("agent job expression phase: %w", exprErr)
	}
	if err := a.advanceChatJobStatus(ctx, trackingJob, models.JobStatusCompactionComplete); err != nil {
		return nil, fmt.Errorf("advance agent job to compaction_complete: %w", err)
	}
	if err := a.advanceChatJobStatus(ctx, trackingJob, models.JobStatusComplete); err != nil {
		return nil, fmt.Errorf("finalize agent job: %w", err)
	}

	return agentMessage, nil
}
