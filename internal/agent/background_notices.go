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
// the run ends. A nil *backgroundRun records nothing, so code outside a run needn't check.
type backgroundRun struct {
	mu      sync.Mutex
	outcome models.BackgroundOutcome
}

type backgroundRunKey struct{}

// withBackgroundRun starts collecting a run's outcomes on ctx.
func withBackgroundRun(ctx context.Context) (context.Context, *backgroundRun) {
	run := &backgroundRun{}
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
	r.outcome.Failed = append(r.outcome.Failed, n)
}

func (r *backgroundRun) succeeded(step models.BackgroundStep) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcome.Succeeded = append(r.outcome.Succeeded, step)
}

// step records how a step ended. Only a failure at, or on the way to, a provider is the
// account's to know about; anything else (the database, a bug) stays in the logs.
func (r *backgroundRun) step(step models.BackgroundStep, err error) {
	if err == nil {
		r.succeeded(step)
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
	case errors.As(err, &missing):
		n := models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeProviderNotConfigured, Provider: missing.Provider.DisplayName()}
		if errors.As(err, &call) {
			n.Model = call.Model
		}
		return n, true
	case errors.As(err, &call):
		return models.BackgroundNotice{Step: step, Code: models.BackgroundNoticeProviderError, Provider: call.Provider, Model: call.Model}, true
	}
	return models.BackgroundNotice{}, false
}

// settleBackgroundRun saves what a run found on the chat. It is best effort: the steps are
// done either way, and their failures are already logged.
func (a *Agent) settleBackgroundRun(ctx context.Context, userID, chatID uuid.UUID, run *backgroundRun) {
	if a.ds == nil || run == nil {
		return
	}
	run.mu.Lock()
	outcome := run.outcome
	run.mu.Unlock()
	if err := a.ds.SettleBackgroundNotices(ctx, userID, chatID, outcome); err != nil {
		a.logger.Warn("failed to record background notices",
			zap.String("chat_id", chatID.String()), zap.Error(err))
	}
}
