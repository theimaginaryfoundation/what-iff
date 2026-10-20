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

	hooks := advancedLoopHooks{
		transcript:   transcript,
		configure:    configure,
		rebuild:      opts.rebuild,
		modelContext: opts.modelContext,
		quotaAllowed: func(ctx context.Context, pending []provider.CallUsage) bool {
			return a.advancedQuotaAllowed(ctx, userID, chatCtx, pending)
		},
		recordStep: func(ctx context.Context, step int) {
			a.recordAdvancedStep(ctx, chatCtx, step)
		},
		saveDraft:     a.advancedDraftSaver(ctx, userID, chatJob, chatCtx),
		contextWindow: models.ContextWindowFor(chatCtx.model),
		config:        advancedLoopConfigFromEnv(a.logger),
	}
	result, toolCalls, generatedAttachments, run, err := a.handleAdvancedLoop(ctx, chatCtx, adapter, hooks)
	chatCtx.agentRun = run
	if err != nil {
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
		if err := a.ds.UpdateJobDraftAgentRun(a.lifecycleCtx, userID, chatJob.ID, draft); err != nil {
			a.logger.Warn("failed to save advanced run draft", zap.String("job_id", chatJob.ID.String()), zap.Error(err))
		}
	}
}

// advancedQuotaAllowed asks the meter whether an Advanced run may make another model call, with
// the calls it has made since its last step record on the context (metering.WithPendingUsage),
// priced as they will be recorded. A meter that sees only the balance would let a run spend a
// whole step past it.
func (a *Agent) advancedQuotaAllowed(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, pending []provider.CallUsage) bool {
	u := metering.Usage{
		UserID:     userID,
		ActionType: chatCtx.meteredActionType(),
		Model:      chatCtx.model,
		AgentLoop:  string(models.AgentLoopAdvanced),
		Calls:      append([]provider.CallUsage(nil), pending...),
		TurnID:     chatCtx.turnID,
	}
	if chatCtx.turnSettings.ReasoningEffortSource != models.SettingSourceUnset {
		u.ReasoningEffort = string(chatCtx.reasoningEffort)
	}
	ctx = metering.WithPendingUsage(ctx, u)
	return a.meter.Check(ctx, userID, chatCtx.modelSubscriptionTier, chatCtx.meteredActionType()).Allowed
}
