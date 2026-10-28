package agent

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// resolveTurnSettings fixes the turn's reasoning effort and agent loop (ADR 0x026), per
// setting: an agent job's values (on ctx), then the active mode's, then the thread's own, then
// its personality's, then the account default. Nothing is copied onto the thread, so the turn
// sees each level's current value. A sandboxed thread takes only its job's and its own values
// (models.TurnSettingLevels.Sandboxed). mode is the turn's active mode (nil for none). Call it
// once the model, personality and mode are final for the turn.
func (a *Agent) resolveTurnSettings(ctx context.Context, chatCtx *chatContext, mode *models.Mood) {
	if chatCtx == nil {
		return
	}
	levels := models.TurnSettingLevels{Job: models.JobTurnSettingsFromContext(ctx)}
	if chatCtx.chat != nil {
		levels.Thread = chatCtx.chat.TurnSettingValues.Level()
		levels.Sandboxed = chatCtx.chat.IsSandboxed()
		if !levels.Sandboxed && a.ds != nil {
			fromPersonality, fromAccount, err := a.ds.InheritedTurnSettings(ctx, chatCtx.chat.UserID, chatCtx.chat.PersonalityID)
			if err != nil {
				// Fail to the thread's own values: nothing inherited is applied, which never
				// starts Advanced or a reasoning effort the user did not ask for on this thread.
				a.logger.Warn("failed to load inherited turn settings; using the thread's own", zap.Error(err))
			}
			levels.Personality = fromPersonality.Level()
			levels.Account = fromAccount.Level()
		}
	}
	if mode != nil && !levels.Sandboxed {
		levels.Mode = mode.TurnSettingValues.Level()
	}
	settings := models.ResolveTurnSettings(levels)
	if settings.Advanced() && a.localLLM {
		// The local backend keeps its own generation pipeline, which has no Advanced loop.
		a.logger.Warn("Advanced agent loop selected but LLM_BACKEND=local has no Advanced loop; running Standard",
			zap.String("agent_loop_source", string(settings.AgentLoopSource)))
		settings.AgentLoop = models.AgentLoopStandard
	}
	chatCtx.turnSettings = settings
	chatCtx.reasoningEffort = models.EffectiveReasoningEffort(chatCtx.reasoningSupport(), settings.ReasoningEffort)
	if settings.ReasoningEffort != "" || settings.AgentLoop != "" {
		a.logger.Info("turn settings resolved",
			zap.String("model", chatCtx.model),
			zap.String("reasoning_effort", string(settings.ReasoningEffort)),
			zap.String("reasoning_effort_source", string(settings.ReasoningEffortSource)),
			zap.String("effective_reasoning_effort", string(chatCtx.reasoningEffort)),
			zap.String("agent_loop", string(settings.AgentLoop)),
			zap.String("agent_loop_source", string(settings.AgentLoopSource)),
		)
	}
}

// threadActiveMood loads the thread's current active mode for runs that do not select one
// (agent jobs, background webhook prompts), so its settings still apply. Nil when the thread
// has no active mode or it cannot be loaded.
func (a *Agent) threadActiveMood(ctx context.Context, chatCtx *chatContext) *models.Mood {
	if chatCtx == nil || chatCtx.chat == nil || chatCtx.chat.ActiveMoodID == nil {
		return nil
	}
	mood, err := a.ds.GetMood(ctx, chatCtx.chat.UserID, *chatCtx.chat.ActiveMoodID)
	if err != nil {
		a.logger.Warn("failed to load the thread's active mode for turn settings", zap.Error(err))
		return nil
	}
	return mood
}

// withTurnSettingsUsage completes a turn's final usage record (ADR 0x026): the reasoning effort
// (only when one was selected and the model honours it, so turns without a selection are metered
// as before), the agent loop, the turn id, and the calls not yet metered, which it then marks as
// metered. An Advanced run has metered its finished steps already (recordAdvancedStep), so its
// final record carries only the rest.
func withTurnSettingsUsage(chatCtx *chatContext, u metering.Usage) metering.Usage {
	if chatCtx == nil {
		return u
	}
	if chatCtx.turnSettings.ReasoningEffortSource != models.SettingSourceUnset {
		u.ReasoningEffort = string(chatCtx.reasoningEffort)
	}
	u.AgentLoop = string(models.AgentLoopStandard)
	if chatCtx.turnSettings.Advanced() {
		u.AgentLoop = string(models.AgentLoopAdvanced)
	}
	u.TurnID = chatCtx.turnID
	if n := len(chatCtx.calls); n > 0 {
		// Tokens is the final call's input on every path (metering.Usage.Tokens).
		u.Tokens = chatCtx.calls[n-1].InputTokens
	}
	u.Calls = chatCtx.takeUnrecordedCalls()
	u.CallsReported = chatCtx.callsReported
	return u
}

// setCalls stores the turn's per-call usage so far, with each call's model filled in.
func (c *chatContext) setCalls(calls []provider.CallUsage) {
	for i := range calls {
		if calls[i].Model == "" {
			calls[i].Model = c.model
		}
	}
	c.calls = calls
	c.callsReported = true
}

// takeUnrecordedCalls returns the calls not yet metered and marks them metered.
func (c *chatContext) takeUnrecordedCalls() []metering.CallUsage {
	if c.recordedCalls >= len(c.calls) {
		return nil
	}
	out := append([]metering.CallUsage(nil), c.calls[c.recordedCalls:]...)
	c.recordedCalls = len(c.calls)
	return out
}

// recordAdvancedStep meters an Advanced step's calls as soon as the step ends, so the quota
// re-check before the next step sees what the run has spent so far, and a run that later fails or
// dies with the server has still paid for the steps it finished.
func (a *Agent) recordAdvancedStep(ctx context.Context, chatCtx *chatContext, step int) {
	if a.meter == nil || chatCtx == nil || chatCtx.skipUsageRecording || chatCtx.chat == nil {
		return
	}
	calls := chatCtx.takeUnrecordedCalls()
	if len(calls) == 0 {
		return
	}
	u := metering.Usage{
		UserID:        chatCtx.chat.UserID,
		ActionType:    chatCtx.meteredActionType(),
		Model:         chatCtx.model,
		ChatID:        chatCtx.chat.ID.String(),
		Tokens:        calls[len(calls)-1].InputTokens,
		Source:        metering.TurnSourceFromContext(ctx),
		AgentLoop:     string(models.AgentLoopAdvanced),
		Calls:         calls,
		CallsReported: true,
		Step:          step,
		TurnID:        chatCtx.turnID,
	}
	if chatCtx.turnSettings.ReasoningEffortSource != models.SettingSourceUnset {
		u.ReasoningEffort = string(chatCtx.reasoningEffort)
	}
	// The calls are marked metered above, so this Record must not fail because Stop or a
	// shutdown has just cancelled the turn's context (the step would then never be billed).
	a.meter.Record(context.WithoutCancel(ctx), chatCtx.meterDecision, u)
}

// withTurnAgentLoop marks an Advanced turn's context for Meter.Check (metering.WithAgentLoop).
func withTurnAgentLoop(ctx context.Context, chatCtx *chatContext) context.Context {
	if chatCtx != nil && chatCtx.turnSettings.Advanced() {
		return metering.WithAgentLoop(ctx, string(models.AgentLoopAdvanced))
	}
	return ctx
}

type subagentReasoningEffortKey struct{}

// withSubagentReasoningEffort carries the parent turn's selected effort to a sub-agent call.
func withSubagentReasoningEffort(ctx context.Context, effort models.ReasoningEffort) context.Context {
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, subagentReasoningEffortKey{}, effort)
}

// subagentReasoningEffort is the effort a sub-agent inherits ("" for none).
func subagentReasoningEffort(ctx context.Context) models.ReasoningEffort {
	v, _ := ctx.Value(subagentReasoningEffortKey{}).(models.ReasoningEffort)
	return v
}

// meteredActionType is the turn's metered action, chat message when unknown.
func (c *chatContext) meteredActionType() string {
	if c.actionType == "" {
		return models.ActionTypeChatMessage
	}
	return c.actionType
}

// reasoningSupport is the turn model's reasoning support: the model row's (with any admin
// override) when known, else derived from the provider and name.
func (c *chatContext) reasoningSupport() models.ReasoningSupport {
	if c.modelReasoningSupport.Valid() {
		return c.modelReasoningSupport
	}
	return models.ReasoningSupportFor(c.modelProvider, c.model)
}
