package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// The Advanced (ReAct) loop, ADR 0x026: steps of the Standard tool loop, each with its round
// counter reset, driven by the model's plan until no work remains.

const (
	defaultAdvancedMaxSteps      = 50
	defaultAdvancedMaxDuration   = 60 * time.Minute
	defaultAdvancedCompactTokens = 120_000
	// Upper bounds on the operator-set limits: a typo (an extra zero) must not let one turn run for
	// days or grow its context without ever compacting. Values above are clamped, with a warning.
	maxAdvancedMaxSteps      = 500
	maxAdvancedMaxDuration   = 6 * time.Hour
	maxAdvancedCompactTokens = 1_000_000
	// advancedMaxStalls ends a run after this many continuation nudges in a row that produced
	// no tool call and no plan change: the model has stopped working, nudging again won't help.
	advancedMaxStalls = 2
	// advancedWorkLogMaxChars bounds the compacted work log.
	advancedWorkLogMaxChars = 24_000
	// Window-relative compaction (shouldCompact): compact once a call uses this share of the
	// model's context window, provided the run has added at least advancedCompactMinGrowth tokens.
	advancedCompactWindowShare = 0.6
	advancedCompactMinGrowth   = 16_000
)

// advancedLoopInstructions are added (as developer context) to every Advanced turn.
const advancedLoopInstructions = `You are working in extended mode: you can take many steps and tool calls to finish this request, so do the work thoroughly rather than stopping early.

For work that takes several steps, keep a checklist with update_plan: write the steps, mark one in_progress while you work on it, and mark each completed or cancelled as you go. You will be asked to continue while any item is pending or in_progress.

Text you write before your final reply is shown to the user as your working notes, not as the answer. When everything is done, write the final reply so it stands on its own (it is the only part kept as your answer) and call no more tools.`

// advancedLoopConfig bounds an Advanced run. Read from the environment on each run so an
// operator can tune it without a restart of the code path (tests set the fields directly).
type advancedLoopConfig struct {
	maxSteps      int
	maxDuration   time.Duration
	compactTokens int64
}

func advancedLoopConfigFromEnv(logger *zap.Logger) advancedLoopConfig {
	return advancedLoopConfigFrom(os.Getenv, logger)
}

// advancedLoopConfigFrom reads the limits through getenv. A set but invalid or non-positive value
// is logged and the default is used, and a value above its upper bound is logged and clamped:
// these limits bound a run's cost, so a typo should be visible and contained.
func advancedLoopConfigFrom(getenv func(string) string, logger *zap.Logger) advancedLoopConfig {
	cfg := advancedLoopConfig{
		maxSteps:      defaultAdvancedMaxSteps,
		maxDuration:   defaultAdvancedMaxDuration,
		compactTokens: defaultAdvancedCompactTokens,
	}
	invalid := func(name, raw string) {
		if logger != nil {
			logger.Warn("ignoring invalid Advanced loop limit; using the default", zap.String("env", name), zap.String("value", raw))
		}
	}
	clamped := func(name, raw, limit string) {
		if logger != nil {
			logger.Warn("Advanced loop limit above its upper bound; clamping", zap.String("env", name), zap.String("value", raw), zap.String("max", limit))
		}
	}
	if raw := strings.TrimSpace(getenv("AGENT_ADVANCED_MAX_STEPS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			cfg.maxSteps = v
			if v > maxAdvancedMaxSteps {
				clamped("AGENT_ADVANCED_MAX_STEPS", raw, strconv.Itoa(maxAdvancedMaxSteps))
				cfg.maxSteps = maxAdvancedMaxSteps
			}
		} else {
			invalid("AGENT_ADVANCED_MAX_STEPS", raw)
		}
	}
	if raw := strings.TrimSpace(getenv("AGENT_ADVANCED_MAX_DURATION")); raw != "" {
		if v, err := time.ParseDuration(raw); err == nil && v > 0 {
			cfg.maxDuration = v
			if v > maxAdvancedMaxDuration {
				clamped("AGENT_ADVANCED_MAX_DURATION", raw, maxAdvancedMaxDuration.String())
				cfg.maxDuration = maxAdvancedMaxDuration
			}
		} else {
			invalid("AGENT_ADVANCED_MAX_DURATION", raw)
		}
	}
	if raw := strings.TrimSpace(getenv("AGENT_ADVANCED_COMPACT_TOKENS")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			cfg.compactTokens = v
			if v > maxAdvancedCompactTokens {
				clamped("AGENT_ADVANCED_COMPACT_TOKENS", raw, strconv.Itoa(maxAdvancedCompactTokens))
				cfg.compactTokens = maxAdvancedCompactTokens
			}
		} else {
			invalid("AGENT_ADVANCED_COMPACT_TOKENS", raw)
		}
	}
	return cfg
}

// advancedLoopHooks are what the loop needs from the generation path that runs it.
type advancedLoopHooks struct {
	transcript *advancedTranscript
	// configure installs the streaming handlers on an adapter (the first one, and any rebuilt
	// by compaction).
	configure func(adapter provider.AgentAdapter)
	// rebuild builds a fresh adapter from a ModelContext (compaction). Nil disables compaction.
	rebuild func(mc *provider.ModelContext) (provider.AgentAdapter, error)
	// modelContext is the turn's original context, which compaction starts again from.
	modelContext *provider.ModelContext
	// quotaAllowed re-checks the account's quota before each model call after the first, given
	// the calls made since the last step record (pending, not yet metered). Nil means always
	// allowed.
	quotaAllowed func(ctx context.Context, pending []provider.CallUsage) bool
	// recordStep meters a finished step's calls (recordAdvancedStep); saveDraft persists the run so
	// far on its job. Either may be nil.
	recordStep func(ctx context.Context, step int)
	saveDraft  func(draft *models.AgentRunDraft)
	// contextWindow is the model's context window in tokens (0 when unknown), for compaction.
	contextWindow int64
	config        advancedLoopConfig
}

// advancedRun is one Advanced run's state. It, and the chatContext it updates (calls,
// recordedCalls, plan, planVersion), are owned by the turn's agent-loop goroutine: nothing else
// reads or writes them while the run is in progress (the live timeline goes through
// jobToolProgress, which has its own lock; step records are made on this goroutine). No locking
// is needed as long as that holds; code that adds another goroutine must add it.
type advancedRun struct {
	a       *Agent
	chatCtx *chatContext
	hooks   advancedLoopHooks
	adapter provider.AgentAdapter
	// retiredUsage is the per-call usage of adapters replaced by compaction.
	retiredUsage []provider.CallUsage
	toolCalls    []*models.ToolCall
	attachments  []*models.FileAttachment
	// compactedAtCall is the last transcript call a compaction folded into the work log.
	compactedAtCall int
	// start is when the run began (the duration limit).
	start time.Time
	// last is the model's latest answer, the reply when the run stops between steps.
	last *provider.GenerateResponse
	// pendingToolResults reports that the last step ended at the round cap with tool results not
	// yet answered: stopping then needs one forced final answer.
	pendingToolResults bool
	// stalls counts continuation nudges in a row that produced no work.
	stalls int
	// step is the current step; endReason is why the run stopped.
	step      int
	endReason models.AgentRunEnd
	// fixedReply reports that the reply is quotaStop's note, not the model's: the last call's text
	// then stays a note in the run record.
	fixedReply bool
}

// handleAdvancedLoop runs the Advanced loop on adapter, which must implement provider.Continuer.
// It returns like handleAgentLoop, plus the run's structured record. On an error the record is
// still returned (and saved as the job's draft), ending in failed or cancelled.
func (a *Agent) handleAdvancedLoop(ctx context.Context, chatCtx *chatContext, adapter provider.AgentAdapter, hooks advancedLoopHooks) (*provider.GenerateResponse, []*models.ToolCall, []*models.FileAttachment, *models.AgentRun, error) {
	run := &advancedRun{a: a, chatCtx: chatCtx, hooks: hooks, adapter: adapter}
	if hooks.configure != nil {
		hooks.configure(adapter)
	}
	result, err := run.loop(ctx)
	if err != nil {
		run.endReason = models.AgentRunEndFailed
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			run.endReason = models.AgentRunEndCancelled
		}
		run.saveDraft()
		return nil, nil, nil, run.record(-1), err
	}
	run.attachments = a.appendPostToolLoopGeneratedAttachments(ctx, chatCtx, run.attachments)
	finalCall := -1
	if hooks.transcript != nil && !run.fixedReply {
		finalCall = hooks.transcript.currentCall()
	}
	return result, run.toolCalls, run.attachments, run.record(finalCall), nil
}

// record is the run's structured record. finalCall is the call whose reply is the message, left
// out of the notes; -1 keeps every call (a run with no final answer).
func (r *advancedRun) record(finalCall int) *models.AgentRun {
	rec := &models.AgentRun{
		Plan:      append([]models.PlanItem(nil), r.chatCtx.plan...),
		EndReason: r.endReason,
	}
	if r.hooks.transcript != nil {
		rec.Steps = r.hooks.transcript.run(finalCall)
	}
	return rec
}

// saveDraft persists the run so far on its job, so a run that stops without the agent finishing
// it (a failure, Stop, a server shutdown) still becomes a partial message.
func (r *advancedRun) saveDraft() {
	if r.hooks.saveDraft == nil {
		return
	}
	draft := &models.AgentRunDraft{Run: *r.record(-1)}
	for _, tc := range r.toolCalls {
		draft.ToolCalls = append(draft.ToolCalls, models.AgentRunToolCall{
			ToolName: tc.ToolName, ToolInput: tc.ToolInput, ToolOutput: tc.ToolOutput, ToolError: tc.ToolError, Step: tc.Step,
		})
	}
	r.hooks.saveDraft(draft)
}

func (r *advancedRun) loop(ctx context.Context) (*provider.GenerateResponse, error) {
	r.start = time.Now()
	for step := 1; ; step++ {
		r.step = step
		r.chatCtx.toolProgress.SetStep(step)
		if step > 1 && r.hooks.transcript != nil {
			r.hooks.transcript.stepHeader(step)
		}
		planBefore := r.chatCtx.planVersion
		res, calls, atts, capped, err := r.a.runToolRounds(ctx, r.chatCtx, r.adapter, toolRoundsOptions{
			step:                step,
			endStepAtCap:        true,
			skipPostAttachments: true,
			beforeCall:          func() error { return r.beforeCall(ctx) },
			afterCall:           r.syncUsage,
		})
		r.syncUsage()
		r.toolCalls = append(r.toolCalls, calls...)
		r.attachments = append(r.attachments, atts...)
		// Meter the step's calls now, also when it failed (they were made), so the quota re-check
		// sees them and a run that dies later has still paid for the work it did.
		if r.hooks.recordStep != nil {
			r.hooks.recordStep(ctx, step)
		}
		if errors.Is(err, errAdvancedQuota) {
			r.end(models.AgentRunEndQuota)
			return r.quotaStop(len(calls) > 0), nil
		}
		if err != nil {
			return nil, err
		}
		r.pendingToolResults = capped

		if capped {
			// The inner round cap ended the step mid-work: that is progress, not a stall.
			r.stalls = 0
		} else {
			r.last = res
			if end := r.judgeAnswer(len(calls) > 0, planBefore); end != "" {
				r.end(end)
				return res, nil
			}
		}
		r.saveDraft()

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if end := r.limitReached(step); end != "" {
			r.end(end)
			return r.finish(ctx)
		}
		r.prepareNextStep(ctx, step)
	}
}

// judgeAnswer decides, once the model has answered without asking for tools, whether the run is
// done (no open plan item) or stalled (advancedMaxStalls nudges in a row with no tool call and no
// plan change). madeToolCalls reports whether the step called any tool.
func (r *advancedRun) judgeAnswer(madeToolCalls bool, planBefore int) models.AgentRunEnd {
	if len(openPlanItems(r.chatCtx.plan)) == 0 {
		return models.AgentRunEndDone
	}
	if !madeToolCalls && r.chatCtx.planVersion == planBefore {
		r.stalls++
	} else {
		r.stalls = 0
	}
	if r.stalls >= advancedMaxStalls {
		return models.AgentRunEndStalled
	}
	return ""
}

// limitReached checks the limits between steps: the time limit and the step limit. The quota is
// checked before every model call instead (beforeCall).
func (r *advancedRun) limitReached(step int) models.AgentRunEnd {
	switch {
	case time.Since(r.start) > r.hooks.config.maxDuration:
		return models.AgentRunEndDuration
	case step >= r.hooks.config.maxSteps:
		return models.AgentRunEndStepLimit
	}
	return ""
}

// prepareNextStep sets the adapter up for the next step: a compacted fresh adapter when the
// context has grown past the threshold, otherwise the same one, continued after an answer (after
// a capped step the pending tool results continue the conversation on their own).
func (r *advancedRun) prepareNextStep(ctx context.Context, step int) {
	if r.shouldCompact() {
		err := r.compact(ctx, step)
		if err == nil {
			// A fresh adapter starts from the original request plus the work log, so there is
			// nothing pending and no continuation to append.
			r.pendingToolResults = false
			return
		}
		r.a.logger.Warn("advanced loop compaction failed; continuing uncompacted", zap.Error(err))
	}
	if !r.pendingToolResults {
		r.adapter.(provider.Continuer).ContinueWith(continuationMessage(r.chatCtx.plan))
	}
}

// finish ends a run that the time or step limit stopped before the model finished (endReason is
// already set). Either last call is quota-checked like any other; when the quota refuses it, the
// run ends as quotaStop does.
//   - Tool results pending: one last answer is forced with tools off, as the Standard loop does.
//   - Otherwise: the model is asked, with tools off, for a reply that says what it did
//     and what is still open, rather than leaving a mid-work note ("next I'll check X") as the
//     answer. The open plan items are also carried into the thread's next turn (previousRunOpenPlan).
func (r *advancedRun) finish(ctx context.Context) (*provider.GenerateResponse, error) {
	if r.pendingToolResults || r.last == nil {
		if err := r.beforeCall(ctx); err != nil {
			return r.quotaStop(true), nil
		}
		final, err := r.adapter.ForceFinalResponse(ctx)
		r.syncUsage()
		if err != nil {
			return nil, fmt.Errorf("failed to force final response: %w", err)
		}
		return final, nil
	}
	r.adapter.(provider.Continuer).ContinueWith(limitSummaryMessage(r.endReason, r.chatCtx.plan))
	if scoper, ok := r.adapter.(provider.ToolScoper); ok {
		scoper.ScopeTools(func(string) bool { return true })
	}
	if err := r.beforeCall(ctx); err != nil {
		return r.last, nil
	}
	res, uses, err := r.adapter.Call(ctx)
	r.syncUsage()
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		r.a.logger.Warn("advanced loop summary call failed; keeping the latest reply", zap.Error(err))
		return r.last, nil
	}
	if res == nil || len(uses) > 0 || strings.TrimSpace(res.Text) == "" {
		return r.last, nil
	}
	return res, nil
}

// end records why the run stopped and logs it.
func (r *advancedRun) end(reason models.AgentRunEnd) {
	r.endReason = reason
	r.a.logger.Info("advanced loop finished",
		zap.String("reason", string(reason)),
		zap.Int("steps", r.step),
		zap.Int("tool_calls", len(r.toolCalls)),
		zap.Int("model_calls", len(r.chatCtx.calls)),
		zap.Int("open_plan_items", len(openPlanItems(r.chatCtx.plan))),
	)
}

// limitSummaryMessage asks a run stopped by a limit for a reply that hands the work back.
func limitSummaryMessage(reason models.AgentRunEnd, plan []models.PlanItem) string {
	limit := "the step limit"
	if reason == models.AgentRunEndDuration {
		limit = "the time limit"
	}
	msg := "You have reached " + limit + " for this run and cannot call any more tools. Write your reply to the user now: say what you completed and what you found, and list what is still left to do so they can ask you to continue."
	if open := openPlanItems(plan); len(open) > 0 {
		msg += "\n\nOpen items in your plan:\n" + formatPlan(open)
	}
	return msg
}

// errAdvancedQuota stops a run before a model call the quota no longer covers.
var errAdvancedQuota = errors.New("advanced run stopped: quota exhausted")

// beforeCall runs before every model call of the run. After the first call it re-checks the
// quota with the calls not yet metered (the current step's), and refuses the call
// (errAdvancedQuota) when the meter does: a run then overshoots its quota by at most the call
// that crossed it, not by a whole step. The turn's own Check covered the first call.
func (r *advancedRun) beforeCall(ctx context.Context) error {
	if r.hooks.quotaAllowed != nil && len(r.chatCtx.calls) > 0 {
		pending := r.chatCtx.calls[min(r.chatCtx.recordedCalls, len(r.chatCtx.calls)):]
		if !r.hooks.quotaAllowed(ctx, pending) {
			return errAdvancedQuota
		}
	}
	if r.hooks.transcript != nil {
		r.hooks.transcript.beginCall()
	}
	return nil
}

// advancedQuotaStopMessage is the reply of a run the quota stopped mid-work.
const advancedQuotaStopMessage = "_I stopped before finishing because this account reached its usage limit. My working notes and the open items of my plan are saved with this message; ask me to continue once more usage is available._"

// quotaStop is the reply of a run the quota stopped before a model call. When the run stopped
// right after an answer (the first call of a new step, nothing done since), that answer stands;
// otherwise it is a fixed note, since another call would spend past the quota. The run's notes,
// plan and tool calls are saved with it either way.
func (r *advancedRun) quotaStop(workSinceAnswer bool) *provider.GenerateResponse {
	if r.last != nil && !r.pendingToolResults && !workSinceAnswer {
		return r.last
	}
	r.fixedReply = true
	return &provider.GenerateResponse{Text: advancedQuotaStopMessage, CreatedAt: time.Now().Unix()}
}

// syncUsage refreshes the turn's per-call usage (metering) from every adapter so far.
func (r *advancedRun) syncUsage() {
	r.chatCtx.calls = r.callUsage()
}

func (r *advancedRun) callUsage() []provider.CallUsage {
	out := append([]provider.CallUsage(nil), r.retiredUsage...)
	if rep, ok := r.adapter.(provider.UsageReporter); ok {
		out = append(out, rep.CallUsage()...)
	}
	return out
}

// shouldCompact reports whether the run's context has grown enough to compact. It compares the
// current adapter's last call with its first (the turn's own context, which compaction cannot
// shrink): compaction triggers when that growth passes compactTokens, or when the last call uses
// more than advancedCompactWindowShare of the model's context window with real growth to remove.
// Measuring growth rather than absolute size keeps a long thread on a large-context model from
// compacting at every step.
func (r *advancedRun) shouldCompact() bool {
	if r.hooks.rebuild == nil || r.hooks.modelContext == nil {
		return false
	}
	rep, ok := r.adapter.(provider.UsageReporter)
	if !ok {
		return false
	}
	usage := rep.CallUsage()
	if len(usage) < 2 {
		return false
	}
	last := usage[len(usage)-1].InputTokens
	growth := last - usage[0].InputTokens
	if r.hooks.config.compactTokens > 0 && growth > r.hooks.config.compactTokens {
		return true
	}
	window := r.hooks.contextWindow
	return window > 0 && float64(last) > advancedCompactWindowShare*float64(window) && growth > advancedCompactMinGrowth
}

// compact replaces the adapter with a fresh one built from the original request plus a work log
// of everything done so far.
func (r *advancedRun) compact(ctx context.Context, step int) error {
	mc := r.hooks.modelContext.Clone()
	mc.InsertBeforeLastUserMessage(provider.SegmentKindDeveloperContext, provider.RoleDeveloper, r.workLog(step), false)
	next, err := r.hooks.rebuild(mc)
	if err != nil {
		return err
	}
	if _, ok := next.(provider.Continuer); !ok {
		return fmt.Errorf("rebuilt adapter %T cannot continue", next)
	}
	if rep, ok := r.adapter.(provider.UsageReporter); ok {
		r.retiredUsage = append(r.retiredUsage, rep.CallUsage()...)
	}
	if r.hooks.configure != nil {
		r.hooks.configure(next)
	}
	r.adapter = next
	if r.hooks.transcript != nil {
		r.compactedAtCall = r.hooks.transcript.currentCall()
	}
	r.a.logger.Info("advanced loop compacted its context", zap.Int("step", step), zap.Int("tool_calls", len(r.toolCalls)))
	return nil
}

// workLog is the developer context a compacted run continues from: the plan, the notes the model
// wrote, and its tool calls with truncated inputs and outputs. The header, plan and notes count
// toward advancedWorkLogMaxChars first; tool calls fill what is left, newest first, so the oldest
// calls are the ones dropped. The kept calls are then written oldest first. The budget is counted in
// bytes (len), a slight overcount for non-ASCII text: it only needs to keep the log well inside the
// model's context, while individual inputs and outputs are cut by runes (TruncateRunes) so no
// character is split.
func (r *advancedRun) workLog(step int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are partway through the user's latest message below, in extended mode (step %d). Earlier work was compacted to save space; here is what you have done so far. Continue from here without repeating finished work.\n", step)
	if len(r.chatCtx.plan) > 0 {
		fmt.Fprintf(&b, "\nYour plan:\n%s\n", formatPlan(r.chatCtx.plan))
	}
	if r.hooks.transcript != nil {
		if notes := r.hooks.transcript.textSince(0); notes != "" {
			fmt.Fprintf(&b, "\nYour notes so far:\n%s\n", agenttools.TruncateRunes(notes, advancedWorkLogMaxChars/3))
		}
	}
	budget := advancedWorkLogMaxChars - b.Len()
	var lines []string
	for i := len(r.toolCalls) - 1; i >= 0 && budget > 0; i-- {
		tc := r.toolCalls[i]
		result := tc.ToolOutput
		if tc.ToolError != "" {
			result = "error: " + tc.ToolError
		}
		line := fmt.Sprintf("- step %d: %s(%s) → %s", tc.Step, tc.ToolName,
			agenttools.TruncateRunes(tc.ToolInput, 300), agenttools.TruncateRunes(result, 600))
		budget -= len(line)
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		if len(lines) < len(r.toolCalls) {
			fmt.Fprintf(&b, "\nTool calls (newest %d of %d; older ones omitted):\n", len(lines), len(r.toolCalls))
		} else {
			b.WriteString("\nTool calls so far:\n")
		}
		for i := len(lines) - 1; i >= 0; i-- {
			b.WriteString(lines[i])
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// continuationMessage asks the model to keep going on its open plan items.
func continuationMessage(plan []models.PlanItem) string {
	return "You still have open items in your plan:\n" + formatPlan(openPlanItems(plan)) +
		"\n\nContinue working on them. If an item is no longer needed, mark it cancelled with update_plan. When everything is done, give your final answer."
}
