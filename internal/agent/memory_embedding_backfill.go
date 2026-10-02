package agent

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"

	"go.uber.org/zap"
)

// DefaultMemoryEmbeddingBackfillInterval is how often the memory embedding
// backfill runs. Hourly is enough to heal a memory whose post-save embedding
// failed without waiting for a deploy, and a run with nothing missing costs one
// query.
const DefaultMemoryEmbeddingBackfillInterval = time.Hour

// memoryEmbeddingBackfillStore is what the backfill needs from the datastore.
type memoryEmbeddingBackfillStore interface {
	TryAcquireSchedulerLeaderLock(ctx context.Context, lockKey int64) (datastore.SchedulerLeaderLock, bool, error)
	BackfillMemoryEmbeddings(ctx context.Context, batchSize int, createEmbeddings datastore.MemoryImportBatchEmbeddingFunc) (datastore.MemoryEmbeddingBackfillStats, error)
}

// MemoryEmbeddingBackfillConfig configures StartMemoryEmbeddingBackfill.
type MemoryEmbeddingBackfillConfig struct {
	// LockKey is the Postgres advisory lock key that elects the one instance
	// running each pass (see server.Config.MemoryEmbeddingBackfillLockKey).
	LockKey int64
	// Interval between passes; DefaultMemoryEmbeddingBackfillInterval when zero.
	Interval time.Duration
}

// StartMemoryEmbeddingBackfill embeds, in the background, every active
// non-Summary memory that has no Embedding row: rows written before memories
// were embedded on save (issue #248), and any whose post-save embedding failed.
// It runs once at start and then every cfg.Interval until ctx is cancelled.
// Each pass runs only on the instance that wins the advisory lock; the others
// skip it quietly, as does a database without advisory locks.
func (a *Agent) StartMemoryEmbeddingBackfill(ctx context.Context, cfg MemoryEmbeddingBackfillConfig) {
	if a.memoryTool == nil {
		a.logger.Warn("memory embedding backfill skipped because memory tool is unavailable")
		return
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultMemoryEmbeddingBackfillInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			runMemoryEmbeddingBackfillPass(ctx, a.ds, cfg.LockKey, a.memoryTool.CreateEmbeddings, a.logger)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// runMemoryEmbeddingBackfillPass runs one backfill pass under the advisory
// lock. It reports whether this instance ran the pass.
func runMemoryEmbeddingBackfillPass(
	ctx context.Context,
	store memoryEmbeddingBackfillStore,
	lockKey int64,
	createEmbeddings datastore.MemoryImportBatchEmbeddingFunc,
	logger *zap.Logger,
) bool {
	if ctx.Err() != nil {
		return false
	}
	lock, acquired, err := store.TryAcquireSchedulerLeaderLock(ctx, lockKey)
	if err != nil {
		// No advisory locks (non-Postgres database) or a transient lock error:
		// skip this pass; the next tick tries again.
		logger.Debug("memory embedding backfill skipped: advisory lock unavailable",
			zap.Int64("lock_key", lockKey),
			zap.Error(err))
		return false
	}
	if !acquired {
		logger.Debug("memory embedding backfill skipped: another instance holds the lock",
			zap.Int64("lock_key", lockKey))
		return false
	}
	defer func() {
		// Release on a fresh context so shutdown cancellation cannot leak the lock
		// session (closing the connection would also drop it).
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			logger.Warn("memory embedding backfill: failed to release advisory lock", zap.Error(err))
		}
	}()

	stats, err := store.BackfillMemoryEmbeddings(ctx, datastore.DefaultMemoryEmbeddingBackfillBatchSize, createEmbeddings)
	fields := []zap.Field{
		zap.Int("embedded", stats.Embedded),
		zap.Int("skipped", stats.Skipped),
		zap.Int("failed", stats.Failed),
	}
	switch {
	case err != nil && ctx.Err() != nil:
		logger.Info("memory embedding backfill interrupted by shutdown", fields...)
	case err != nil:
		logger.Warn("memory embedding backfill stopped early", append(fields, zap.Error(err))...)
	case stats.Embedded+stats.Skipped+stats.Failed > 0:
		logger.Info("memory embedding backfill finished", fields...)
	default:
		logger.Debug("memory embedding backfill: nothing to do")
	}
	return true
}
