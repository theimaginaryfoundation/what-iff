package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// runAdvancedGeneration is runGeneration for an Advanced turn (ADR 0x026). Everything the model
// writes before its final reply streams live as reasoning (the transcript); the final call's reply
// is saved as the message, with the run's steps, plan and end reason as its agent_run. Each step
// is metered as it finishes and the run so far is saved on the job, so a run that never finishes
// still bills its work and becomes a partial message.
func (a *Agent) runAdvancedGeneration(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, adapter provider.AgentAdapter, opts generationOptions) (*models.ChatMessage, *provider.GenerateResponse, error) {
	reasoningBuffer := newJobDraftReasoningBuffer(a.lifecycleCtx, a.ds, a.logger, chatJob, jobDraftDeltaFlushMinChars, jobDraftDeltaFlushMaxWait)
	defer reasoningBuffer.Flush()
	transcript := newAdvancedTranscript(reasoningBuffer.HandleDelta, reasoningBuffer.ResetReasoning)

	current := adapter
	configure := func(ad provider.AgentAdapter) {
		current = ad
		ad.SetTextDeltaHandler(transcript.textDelta)
		if streamer, ok := ad.(provider.ReasoningStreamer); ok {
			streamer.SetReasoningStream(provider.ReasoningStream{
				OnDelta:       transcript.nativeDelta,
				OnDiscardCall: transcript.discardCallNative,
			})
		}
	}
	// Persist buffered notes before each tool row, so they reach the client ahead of it.
	chatCtx.toolProgress = a.newChatToolProgress(chatJob, reasoningBuffer.Flush)
	chatCtx.toolProgress.Seed(chatCtx.memoryProgress.Entries())
	if progress := chatCtx.toolProgress; progress != nil {
		defer progress.Close()
	}

	draftJob := chatJob
	if draftJob == nil {
		draftJob = chatCtx.draftJob
	}
	hooks := advancedLoopHooks{
		transcript:   transcript,
		configure:    configure,
		rebuild:      opts.rebuild,
		modelContext: opts.modelContext,
		quotaAllowed: func(ctx context.Context, pending []provider.CallUsage) (bool, error) {
			return a.advancedQuotaAllowed(ctx, userID, chatCtx, pending)
		},
		recordStep: func(ctx context.Context, step int) { // recordAdvancedStep detaches from ctx's cancellation
			a.recordAdvancedStep(ctx, chatCtx, step)
		},
		saveDraft:     a.advancedDraftSaver(ctx, userID, draftJob, chatCtx),
		saveToolCall:  a.advancedToolCallSaver(ctx, userID, draftJob),
		contextWindow: models.ContextWindowFor(chatCtx.model),
		config:        a.advancedLoopConfigFor(ctx, userID),
	}
	result, toolCalls, generatedAttachments, run, err := a.handleAdvancedLoop(ctx, chatCtx, adapter, hooks)
	chatCtx.agentRun = run
	if err != nil {
		a.settleUnfinishedAdvancedRun(ctx, userID, chatMessage.ChatID, chatCtx, current, opts, toolCalls, hooks.saveToolCall)
		return nil, nil, fmt.Errorf("%s advanced loop failed: %w", opts.provider, err)
	}
	if result == nil {
		return nil, nil, fmt.Errorf("%s advanced loop returned nil response with no error", opts.provider)
	}
	result.Text = strings.TrimSpace(result.Text)
	return a.finishGeneration(ctx, userID, chatMessage, chatCtx, current, opts, result, toolCalls, generatedAttachments)
}

// advancedDraftSaver returns the hook that saves an Advanced run's draft on its job, or nil when
// the turn has no job. The draft carries the generation metadata so a partial message finalized
// without the agent (a server shutdown) is attributed like a finished one. Saves use the
// lifecycle context: the draft written as a run is cancelled must still land.
func (a *Agent) advancedDraftSaver(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatCtx *chatContext) func(*models.AgentRunDraft) {
	if chatJob == nil || a.ds == nil {
		return nil
	}
	personality := ""
	if chatCtx.chat != nil {
		personality = a.resolvePersonalityName(ctx, userID, chatCtx.chat.PersonalityID)
	}
	moodID := ""
	if id := activeMoodID(chatCtx.activeMood); id != nil && *id != uuid.Nil {
		moodID = id.String()
	}
	return func(draft *models.AgentRunDraft) {
		draft.GenerationModel = chatCtx.model
		draft.GenerationPersonality = personality
		draft.GenerationMoodID = moodID
		writeCtx, cancel := a.draftWriteContext(ctx)
		defer cancel()
		if err := a.ds.UpdateJobDraftAgentRun(writeCtx, userID, chatJob.ID, draft); err != nil {
			a.logger.Warn("failed to save advanced run draft", zap.String("job_id", chatJob.ID.String()), zap.Error(err))
		}
	}
}

// advancedToolCallSaver returns the hook that saves an Advanced run's finished tool calls as
// draft rows of its job, or nil when the turn has no job.
func (a *Agent) advancedToolCallSaver(ctx context.Context, userID uuid.UUID, chatJob *models.Job) func(*models.ToolCall) error {
	if chatJob == nil || a.ds == nil {
		return nil
	}
	return func(tc *models.ToolCall) error {
		writeCtx, cancel := a.draftWriteContext(ctx)
		defer cancel()
		return a.ds.CreateJobDraftToolCall(writeCtx, userID, chatJob.ID, tc)
	}
}

// draftWriteContext is the context an Advanced run's draft writes use: detached from the turn's
// and the server's cancellation (Stop and Shutdown cancel those first, and the work done so far
// must still be saved), bounded by jobTerminalPersistTimeout.
func (a *Agent) draftWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), jobTerminalPersistTimeout)
}

// advancedQuotaAllowed asks the meter whether an Advanced run may make another model call, with
// the calls it has made since its last step record on the context (metering.WithPendingUsage),
// priced as they will be recorded. A meter that sees only the balance would let a run spend a
// whole step past it. A Check that cannot decide (Decision.Err) is retried once, since the run
// would otherwise fail on one transient database error; a second failure is returned.
func (a *Agent) advancedQuotaAllowed(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, pending []provider.CallUsage) (bool, error) {
	u := metering.Usage{
		UserID:        userID,
		ActionType:    chatCtx.meteredActionType(),
		Model:         chatCtx.model,
		AgentLoop:     string(models.AgentLoopAdvanced),
		Calls:         append([]provider.CallUsage(nil), pending...),
		CallsReported: true,
		TurnID:        chatCtx.turnID,
	}
	if chatCtx.turnSettings.ReasoningEffortSource != models.SettingSourceUnset {
		u.ReasoningEffort = string(chatCtx.reasoningEffort)
	}
	ctx = metering.WithPendingUsage(ctx, u)
	d := a.meter.Check(ctx, userID, chatCtx.modelSubscriptionTier, chatCtx.meteredActionType())
	if d.Err != nil && ctx.Err() == nil {
		d = a.meter.Check(ctx, userID, chatCtx.modelSubscriptionTier, chatCtx.meteredActionType())
	}
	if d.Err != nil {
		return false, d.Err
	}
	return d.Allowed, nil
}

// settleUnfinishedAdvancedRun does for a run that failed or was stopped what finishGeneration
// does for a finished one, as far as it applies: the provider-native web searches are saved with
// the partial reply (as draft tool calls of the job), and the run's web searches are metered, which
// would otherwise only happen with the final record.
func (a *Agent) settleUnfinishedAdvancedRun(ctx context.Context, userID, chatID uuid.UUID, chatCtx *chatContext, adapter provider.AgentAdapter, opts generationOptions, toolCalls []*models.ToolCall, saveToolCall func(*models.ToolCall) error) {
	if opts.mergeToolCalls != nil && saveToolCall != nil {
		for _, tc := range opts.mergeToolCalls(nil) {
			tc.ID = uuid.New()
			if err := saveToolCall(tc); err != nil {
				a.logger.Warn("failed to save a native web search with the partial reply", zap.Error(err))
			}
		}
	}
	chatCtx.webSearchCount = a.turnWebSearchCount(adapter, toolCalls)
	if opts.vendorWebSearches != nil && !a.FirstPartyWebSearch() {
		chatCtx.webSearchCount = opts.vendorWebSearches()
	}
	if chatCtx.skipUsageRecording || a.meter == nil {
		return
	}
	if usage, ok := a.webSearchUsage(userID, chatID, chatCtx, chatCtx.meteredActionType()); ok {
		a.meter.Record(context.WithoutCancel(ctx), chatCtx.meterDecision, usage)
	}
}
