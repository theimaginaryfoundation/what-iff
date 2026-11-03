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
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/pushnotify"
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
		// Registered before the panic guard so it runs after it; a panic keeps the initial outcome.
		finish := a.startJobRun(runCtx, backgroundJob)
		outcome := telemetry.JobOutcomePanic
		defer func() { finish(outcome) }()
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
			outcome = telemetry.JobOutcomeFailed
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
		// As on the chat path, a cancelled run context means the job was cancelled even when the
		// error it surfaced as (a provider or datastore failure) doesn't wrap context.Canceled.
		cancelled := runErr != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runCtx.Err(), context.Canceled))
		outcome = chatJobOutcome(runErr)
		if cancelled {
			outcome = telemetry.JobOutcomeCancelled
		}
		if runErr != nil {
			if cancelled {
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
			_, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, providerkeys.FailureText(runErr))
			if failErr == nil {
				a.recordJobFailureCode(runCtx, userID, jobID, runErr)
			}
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
	ctx, err := detachedUserContext(ctx)
	if err != nil {
		return nil, err
	}
	return a.handleEphemeralPrompt(ctx, chatID, prompt, modelOverrideID, personalityOverrideID, ritualIDs, trackingJob, models.ActionTypeJobRun, telemetry.CallPathAgentJob, ephemeralPromptOptions{})
}

// HandleEphemeralPromptSync runs a synchronous autonomous prompt against a chat
// without persisting the injected user prompt. Only the assistant response is saved.
func (a *Agent) HandleEphemeralPromptSync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessage, error) {
	ctx, err := detachedUserContext(ctx)
	if err != nil {
		return nil, err
	}
	return a.handleEphemeralPrompt(ctx, chatID, prompt, modelOverrideID, personalityOverrideID, nil, nil, models.ActionTypeChatMessage, telemetry.CallPathUserChat, ephemeralPromptOptions{})
}

// detachedUserContext copies the caller's user ID and timezone onto a fresh Background context,
// so a synchronous run finishes even if the caller's context ends first. It returns an error
// when ctx carries no user ID.
func detachedUserContext(ctx context.Context) (context.Context, error) {
	detached, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}
	// An agent job's reasoning effort / agent loop (ADR 0x026) and its name (ADR 0x040) ride on
	// the context.
	return models.CopyJobRun(ctx, models.CopyJobTurnSettings(ctx, detached)), nil
}

// handleEphemeralPrompt runs the turn on ctx as given, which must carry the user ID: callers
// decide whether it is detached from their own lifetime. The async agent_job_run worker passes
// its cancellable run context, so cancelling that job stops the turn; the sync entry points
// pass a detached one (detachedUserContext).
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
) (_ *models.ChatMessage, retErr error) {
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

	// Turn gate: queue behind earlier turns in this chat (#254); see beginEphemeralChatTurn.
	endTurn, ticket, err := a.beginEphemeralChatTurn(ctx, userID, chatID, trackingJob)
	if err != nil {
		return nil, err
	}
	// The thread's Stop reaches a scheduled run through its ticket (ADR 0x039).
	// A scheduled run releases the Stop and ends its ticket early, once its reply is saved (below);
	// releaseStop and endTurn are idempotent, so this cleanup covers every other exit.
	ctx, releaseStop, endRun := a.stoppableTurnTicket(ctx, userID, ticket)
	defer func() {
		retErr = stoppedTurnError(ctx, retErr)
		endRun()
		endTurn(retErr)
	}()
	// The job the turn streams its draft, reasoning and tool timeline on, and an Advanced run
	// keeps its draft on (ADR 0x026): the tracking job, or a scheduled run's queue ticket, so the
	// thread shows a scheduled run live like a reply to the user (ADR 0x039).
	draftJob := trackingJob
	if draftJob == nil {
		draftJob = ticket
	}

	// Rehydration gate: stall if this thread's import summary is still in flight (no-op otherwise).
	a.WaitForThreadRehydration(ctx, userID, chatID)
	// The prompt is "sent" when the turn starts, not when it queued: its time is shown to the
	// model, and a checkpoint treats messages saved after it as unseen (checkpointWindowStart).
	ephemeralUserMessage.SentAt = time.Now()

	// The memory load shows live on the run's ticket, as on a user's turn.
	chatCtx, err := a.prepareChatContext(ctx, userID, ephemeralUserMessage, a.newMemoryLoadProgress(draftJob))
	if err != nil {
		return nil, err
	}

	// Overrides below apply only to this run; the save after inference writes continuity only, so
	// they never reach the Chat row.
	if modelOverrideID != nil {
		model, err := a.ds.GetModel(ctx, *modelOverrideID)
		if err == nil && model != nil {
			chatCtx.chat.ModelID = model.ID
			chatCtx.chat.ModelName = model.Name
			chatCtx.chat.ToolsEnabled = model.ToolSupport
			chatCtx.model = model.Name
			chatCtx.modelProvider = model.Provider
			chatCtx.modelReasoningSupport = model.ReasoningSupport
			chatCtx.modelSubscriptionTier = model.SubscriptionTier
			chatCtx.modelVisionSupport = model.VisionSupport
		} else if err != nil && !errors.Is(err, datastore.ErrModelNotFound) {
			return nil, fmt.Errorf("failed to resolve model override: %w", err)
		}
	}
	if personalityOverrideID != nil {
		personality, err := a.ds.GetPersonality(ctx, userID, *personalityOverrideID)
		if err == nil && personality != nil {
			chatCtx.chat.PersonalityID = personality.ID
			chatCtx.chat.SystemPrompt = personality.SystemPrompt
			if !chatCtx.chat.IsSandboxed() { // see datastore.toChatModel: no scratchpad in a sandboxed chat
				chatCtx.chat.Scratchpad = personality.Scratchpad
			}
		}
	}

	// Reasoning effort and agent loop: the job's (on ctx), then the thread's active mode, then
	// the thread (ADR 0x026). This path does not select a mode, so the current one is read.
	a.resolveTurnSettings(ctx, chatCtx, a.threadActiveMood(ctx, chatCtx))
	// The job's skills are this message's skills; the index and loaded skills follow the chat.
	a.prepareTurnSkills(ctx, userID, chatCtx, ephemeralUserMessage)
	if opts.skipUsageRecording {
		// An unmetered turn (the welcome message) never runs Advanced or a selected effort: it
		// would be work nobody pays for.
		chatCtx.turnSettings = models.TurnSettings{}
		chatCtx.reasoningEffort = ""
	}
	ctx = withTurnAgentLoop(ctx, chatCtx)

	// Quota gate — checked after overrides are resolved so we use the effective model tier.
	// actionType controls whether free-tier chat eligibility can apply.
	qd := metering.Decision{Allowed: true}
	if !opts.skipQuotaCheck {
		qd = a.meter.Check(ctx, userID, chatCtx.modelSubscriptionTier, actionType)
		if !qd.Allowed {
			a.recordQuotaRejection(ctx)
			return nil, fmt.Errorf("%w for user %s", ErrQuotaExceeded, userID)
		}
	}
	chatCtx.meterDecision = qd
	chatCtx.actionType = actionType
	chatCtx.skipUsageRecording = opts.skipUsageRecording
	chatCtx.draftJob = draftJob
	if draftJob != nil {
		chatCtx.turnID = draftJob.ID.String()
	}

	doneBuildContext := a.timeTurnStage(ctx, turnStageBuildContext)
	modelContext, err := a.buildModelContextForChatMessage(ctx, userID, ephemeralUserMessage, chatCtx, nil)
	doneBuildContext()
	if err != nil {
		return nil, fmt.Errorf("failed to build model context: %w", err)
	}

	agentMessage, resp, err := a.generateAssistantForMessage(ctx, userID, draftJob, ephemeralUserMessage, chatCtx, modelContext)
	if err != nil {
		a.finalizeEphemeralAdvancedDraft(ctx, userID, draftJob, chatID, err)
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("unexpected nil response")
	}
	ephemeralUserMessage.Tokens = resp.InputTokens

	// Update chat metadata for thread continuity + sorting.
	chatCtx.chat.ResponseID = &resp.ID
	lastMessageTime := resp.CreatedTime()
	chatCtx.chat.LastMessageTime = &lastMessageTime

	// Persist only thread continuity: chatCtx.chat carries this job's model/personality overrides
	// and the turn-start copy of everything else, none of which may be written back (#347).
	if err := a.ds.UpdateChatContinuity(ctx, userID, chatCtx.chat.ID, resp.ID, &lastMessageTime); err != nil {
		return nil, fmt.Errorf("failed to update chat metadata: %w", err)
	}

	// The reply and the chat's response chain are saved, so a turn queued behind this one may start;
	// the expression pick and checkpoint below overlap it. A tracked job opens the gate through its
	// own status (advanceJobInferenceComplete). A scheduled run has no job row, so its queue ticket
	// is ended here, pointing at the reply (and with its draft cleared: the reply took the run's
	// draft tool calls) so a client following the ticket fetches the reply as it does a user turn's.
	if trackingJob == nil {
		if ticket != nil && a.ds != nil {
			if err := a.advanceJobInferenceComplete(ctx, ticket, agentMessage.ID); err != nil {
				a.logger.Warn("failed to record the reply on the turn ticket", zap.String("job_id", ticket.ID.String()), zap.Error(err))
			}
		}
		releaseStop()
		endTurn(nil)
	}
	if err := a.advanceJobInferenceComplete(ctx, trackingJob, agentMessage.ID); err != nil {
		return nil, fmt.Errorf("failed to advance agent job after inference: %w", err)
	}

	exprErr := a.applyExpressionPhase(ctx, userID, trackingJob, chatCtx, modelContext, prompt, agentMessage)

	donePostProcess := a.timeTurnStage(ctx, turnStagePostProcess)
	a.postMessageProcessing(ctx, userID, ephemeralUserMessage, agentMessage, chatCtx, modelContext, actionType, qd, opts.skipUsageRecording)
	donePostProcess()

	if exprErr != nil {
		return nil, fmt.Errorf("agent job expression phase: %w", exprErr)
	}
	if err := a.advanceChatJobStatus(ctx, trackingJob, models.JobStatusCompactionComplete); err != nil {
		return nil, fmt.Errorf("advance agent job to compaction_complete: %w", err)
	}
	if err := a.advanceChatJobStatus(ctx, trackingJob, models.JobStatusComplete); err != nil {
		return nil, fmt.Errorf("finalize agent job: %w", err)
	}

	// The prompt was ephemeral, so there is no saved user message to point at.
	a.fireReplyHook(userID, agentMessage, nil, trackingJob, callPath)

	// Notify the user of this completed reply. Gated to the agent-job path
	// (callPath) — scheduled/autonomous jobs and webhook background-mode
	// invocations, the replies that land after the app is closed; interactive
	// chat completes elsewhere (runUserChatPostInferencePhases) and is not
	// notified. Only pointers are passed; the client fetches the content.
	//
	// Runs on a detached goroutine with panic recovery so a slow or faulty
	// notifier implementation can neither stall nor crash this job goroutine.
	// a.lifecycleCtx outlives the request but is cancelled on shutdown. Skipped
	// entirely when no push implementation is wired (a.pushEnabled == false).
	if a.pushEnabled && callPath == telemetry.CallPathAgentJob {
		ev := pushnotify.Event{
			Kind:      pushnotify.EventKindAgentReply,
			UserID:    userID,
			ChatID:    agentMessage.ChatID,
			MessageID: agentMessage.ID,
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					a.logger.Error("push notifier panicked",
						zap.Any("recover", r),
						zap.ByteString("stack", debug.Stack()))
				}
			}()
			a.pushNotifier.Notify(a.lifecycleCtx, ev)
		}()
	}

	return agentMessage, nil
}

// ErrTurnStopped is the error of a scheduled run stopped from its thread (ADR 0x039). It wraps
// context.Canceled, so the run's ticket ends cancelled like any cancelled turn, but only a Stop
// produces it: a run cut short because its caller's context ended (a worker shutting down) carries
// plain context.Canceled, and the scheduler records that as a failure, not as a stop.
var ErrTurnStopped = fmt.Errorf("stopped from its thread: %w", context.Canceled)

// stoppableTurnTicket makes a scheduled run's queue ticket stoppable like a user turn's job: the
// thread's Stop (CancelJob) cancels it directly when it runs in this process, and a Stop handled by
// another instance reaches it through the ticket's status (watchChatJobCancel). Either cancels the
// returned context with ErrTurnStopped as its cause (see stoppedTurnError).
//
// release ends both paths once the reply is saved, so a late Stop no longer cuts the run's
// post-processing short. end is for the turn's return only: it releases first, then frees the
// context, so the context's own cancellation is never reported as a stop. Both may be called more
// than once. A turn with no ticket (a tracked job, which its own worker registers, or no turn gate)
// gets ctx back with no-op functions.
func (a *Agent) stoppableTurnTicket(ctx context.Context, userID uuid.UUID, ticket *models.Job) (_ context.Context, release, end func()) {
	if ticket == nil {
		return ctx, func() {}, func() {}
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	stop := func() { cancel(ErrTurnStopped) }
	watchCtx, endWatch := context.WithCancel(runCtx)
	a.registerRunningJobCancel(ticket.ID, userID, stop)
	go a.watchChatJobCancel(watchCtx, userID, ticket.ID, stop)
	release = func() {
		a.unregisterRunningJobCancel(ticket.ID)
		endWatch()
	}
	end = func() {
		release()
		cancel(nil)
	}
	return runCtx, release, end
}

// stoppedTurnError reports a turn that was stopped as stopped (ErrTurnStopped). A Stop that lands
// mid-call can surface as context.Canceled, or as the provider's or datastore's error, and the
// ticket and the scheduler must still record a stop, not a failure. A context cancelled for any
// other reason leaves err as it is.
func stoppedTurnError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrTurnStopped) || !errors.Is(context.Cause(ctx), ErrTurnStopped) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrTurnStopped, err)
}

// finalizeEphemeralAdvancedDraft saves a failed or stopped agent-job Advanced run (ADR 0x026) as a
// partial reply from its draft, as a chat turn's failure path does. A turn without an Advanced
// draft is left to its caller's status handling.
func (a *Agent) finalizeEphemeralAdvancedDraft(ctx context.Context, userID uuid.UUID, draftJob *models.Job, chatID uuid.UUID, cause error) {
	if draftJob == nil || a.ds == nil {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobTerminalPersistTimeout)
	defer cancel()
	cancelled := errors.Is(cause, context.Canceled) || ctx.Err() != nil
	if _, err := a.ds.FinalizeAdvancedDraft(persistCtx, userID, draftJob.ID, chatID, cancelled, cause.Error(), failureCode(cause)); err != nil {
		a.logger.Warn("failed to save the Advanced run's partial reply", zap.String("job_id", draftJob.ID.String()), zap.Error(err))
	}
}
