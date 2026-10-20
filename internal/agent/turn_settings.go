package agent

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// resolveTurnSettings fixes the turn's reasoning effort and agent loop (ADR 0x026): an agent
// job's values (on ctx) win, then the active mode's, then the thread's own (seeded from the
// personality or account default when the thread was created). mode is the turn's active mode
// (nil for none). Call it once the model and the mode are final for the turn.
func (a *Agent) resolveTurnSettings(ctx context.Context, chatCtx *chatContext, mode *models.Mood) {
	if chatCtx == nil {
		return
	}
	var threadLevel, modeLevel models.TurnSettingLevel
	if chatCtx.chat != nil {
		threadLevel = chatCtx.chat.TurnSettingValues.Level()
	}
	if mode != nil {
		modeLevel = mode.TurnSettingValues.Level()
	}
	settings := models.ResolveTurnSettings(threadLevel, modeLevel, models.JobTurnSettingsFromContext(ctx))
	if settings.Advanced() && a.localLLM {
		// The local backend keeps its own generation pipeline, which has no Advanced loop.
		a.logger.Warn("Advanced agent loop selected but LLM_BACKEND=local has no Advanced loop; running Standard",
			zap.String("agent_loop_source", string(settings.AgentLoopSource)))
		settings.AgentLoop = models.AgentLoopStandard
	}
	chatCtx.turnSettings = settings
	chatCtx.reasoningEffort = models.EffectiveReasoningEffort(models.ReasoningSupportFor(chatCtx.modelProvider, chatCtx.model), settings.ReasoningEffort)
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
	u.Calls = chatCtx.takeUnrecordedCalls()
	return u
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
		UserID:     chatCtx.chat.UserID,
		ActionType: chatCtx.meteredActionType(),
		Model:      chatCtx.model,
		ChatID:     chatCtx.chat.ID.String(),
		Source:     metering.TurnSourceFromContext(ctx),
		AgentLoop:  string(models.AgentLoopAdvanced),
		Calls:      calls,
		Step:       step,
		TurnID:     chatCtx.turnID,
	}
	if chatCtx.turnSettings.ReasoningEffortSource != models.SettingSourceUnset {
		u.ReasoningEffort = string(chatCtx.reasoningEffort)
	}
	a.meter.Record(ctx, chatCtx.meterDecision, u)
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
