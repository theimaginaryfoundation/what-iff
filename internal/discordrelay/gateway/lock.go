package gateway

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
)

// DefaultLockKey is the advisory lock key for gateway leadership. The agent-job
// scheduler's default is 80920031; this is the next one, so the two never
// contend. Override with DISCORD_GATEWAY_LOCK_KEY.
const DefaultLockKey int64 = 80920032

// AdvisoryLocker leads through the datastore's Postgres advisory lock, the same
// mechanism the agent-job scheduler uses for its leadership.
type AdvisoryLocker struct {
	DS  *datastore.Datastore
	Key int64
}

func (l AdvisoryLocker) TryAcquire(ctx context.Context) (Lock, bool, error) {
	lock, ok, err := l.DS.TryAcquireSchedulerLeaderLock(ctx, l.Key)
	if err != nil || !ok {
		return nil, ok, err
	}
	return lock, true, nil
}

// SingleInstanceLocker always leads. For a single-process deployment, or a local
// database without advisory locks (sqlite); never with more than one API task.
type SingleInstanceLocker struct{}

func (SingleInstanceLocker) TryAcquire(context.Context) (Lock, bool, error) {
	return alwaysHealthy{}, true, nil
}

type alwaysHealthy struct{}

func (alwaysHealthy) IsHealthy(ctx context.Context) (bool, error) { return ctx.Err() == nil, nil }
func (alwaysHealthy) Release(context.Context) error               { return nil }
