package remotesync

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// DefaultTick is how often the runner looks for due accounts. An account is due once its
// Interval has passed, so the tick only bounds how late a run can start.
const DefaultTick = time.Minute

// Runner syncs due accounts on a timer until ctx ends. Several API instances can each run one:
// an account is only run by whoever claims its lease.
type Runner struct {
	Syncer *Syncer
	Tick   time.Duration
	Logger *zap.Logger
}

// Run blocks until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	tick := r.Tick
	if tick <= 0 {
		tick = DefaultTick
	}
	logger := r.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	runOnce := func() {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("remote sync: runner recovered from panic", zap.Any("panic", v))
			}
		}()
		r.Syncer.RunDue(ctx)
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	runOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
