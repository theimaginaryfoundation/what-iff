package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// backgroundRun collects what a run of background steps on one chat (a checkpoint) found, so
// the account can be told about failures and fallbacks (ADR 0x027 section 6) in one write when
// the run ends. A step is reported once per run however many calls it makes, and a failure
// anywhere in the run outweighs a success. A nil *backgroundRun records nothing, so code
// outside a run needn't check.
type backgroundRun struct {
	mu        sync.Mutex
	failures  map[models.BackgroundStep]models.BackgroundNotice
	successes map[models.BackgroundStep]bool
}

type backgroundRunKey struct{}

// withBackgroundRun starts collecting a run's outcomes on ctx.
func withBackgroundRun(ctx context.Context) (context.Context, *backgroundRun) {
	run := &backgroundRun{failures: map[models.BackgroundStep]models.BackgroundNotice{}, successes: map[models.BackgroundStep]bool{}}
	return context.WithValue(ctx, backgroundRunKey{}, run), run
}

// backgroundRunFrom is the run ctx is part of, or nil.
func backgroundRunFrom(ctx context.Context) *backgroundRun {
	run, _ := ctx.Value(backgroundRunKey{}).(*backgroundRun)
	return run
}

func (r *backgroundRun) failed(n models.BackgroundNotice) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, seen := r.failures[n.Step]; !seen {
		r.failures[n.Step] = n
	}
}

func (r *backgroundRun) succeeded(step models.BackgroundStep) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.successes[step] = true
}

// outcome is the run's result: each step once, failed if it failed anywhere.
func (r *backgroundRun) outcome() models.BackgroundOutcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out models.BackgroundOutcome
	for _, n := range r.failures {
		out.Failed = append(out.Failed, n)
	}
	for step := range r.successes {
		if _, failed := r.failures[step]; !failed {
			out.Succeeded = append(out.Succeeded, step)
		}
	}
	return out
}

// step records how a step ended. Only a failure the account can fix is theirs to know about: at,
// or on the way to, a provider, or no model chosen for the step's role. Anything else (the
// database, a bug, the checkpoint running out of time or the server stopping) stays in the logs.
// A role switched off counts as success, so its earlier notices clear.
func (r *backgroundRun) step(step models.BackgroundStep, err error) {
	if err == nil {
		r.succeeded(step)
		return
	}
	if errors.Is(err, ErrUtilityRoleOff) {
		// Switched off by choice: nothing to tell, and nothing earlier still to show.
		r.succeeded(step)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if n, ok := backgroundNoticeFor(step, err); ok {
		r.failed(n)
	}
}

// backgroundNoticeFor says what an account should know about a failed step, if anything.
func backgroundNoticeFor(step models.BackgroundStep, err error) (models.BackgroundNotice, bool) {
	var call *UtilityCallError
	var missing *providerkeys.NotConfiguredError
	switch {
	case errors.Is(err, ErrUtilityRoleNotSet):
		return models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeModelNotSet}, true
	case errors.Is(err, errUtilityProviderMissing) && errors.As(err, &call):
		// Nothing was asked: this deployment can't reach the provider at all.
		return models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeProviderNotConfigured, Provider: call.Provider, Model: call.Model}, true
	case errors.As(err, &missing):
		n := models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeProviderNotConfigured, Provider: missing.Provider.DisplayName()}
		if errors.As(err, &call) {
			n.Model = call.Model
		}
		return n, true
	case errors.Is(err, models.ErrUtilityModelUnavailable):
		return models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeModelUnavailable}, true
	case errors.As(err, &call):
		return models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeProviderError, Provider: call.Provider, Model: call.Model}, true
	}
	return models.BackgroundNotice{}, false
}

// withBackgroundNotices runs background steps on a chat with a run on their context, then
// saves what the run found on the chat, even if the steps ran out of time.
func (a *Agent) withBackgroundNotices(ctx context.Context, userID, chatID uuid.UUID, steps func(context.Context)) {
	runCtx, run := withBackgroundRun(ctx)
	defer a.settleBackgroundRun(ctx, userID, chatID, run)
	steps(runCtx)
}

// settleBackgroundRun saves what a run found on the chat, within the time any job-ending write
// gets. It is best effort: the steps are done either way, and their failures are already
// logged.
func (a *Agent) settleBackgroundRun(ctx context.Context, userID, chatID uuid.UUID, run *backgroundRun) {
	if a.ds == nil || run == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobTerminalPersistTimeout)
	defer cancel()
	if err := a.ds.SettleBackgroundNotices(ctx, userID, chatID, run.outcome()); err != nil {
		a.logger.Warn("failed to record background notices",
			zap.String("chat_id", chatID.String()), zap.Error(err))
	}
}

// recordTurnUtility saves at once how one of a turn's own utility calls on a chat went (a step
// such as models.BackgroundStepChatName): its notice when it failed in a way the account can
// fix, or the end of an earlier one when it succeeded. Best effort, like settleBackgroundRun.
func (a *Agent) recordTurnUtility(ctx context.Context, userID, chatID uuid.UUID, step models.BackgroundStep, err error) {
	if chatID == uuid.Nil {
		return
	}
	run := &backgroundRun{failures: map[models.BackgroundStep]models.BackgroundNotice{}, successes: map[models.BackgroundStep]bool{}}
	run.step(step, err)
	if out := run.outcome(); len(out.Failed) == 0 && len(out.Succeeded) == 0 {
		return
	}
	a.settleBackgroundRun(ctx, userID, chatID, run)
}
