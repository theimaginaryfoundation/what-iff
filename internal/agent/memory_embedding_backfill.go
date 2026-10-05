package agent

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"

	"go.uber.org/zap"
)

// memoryEmbeddingBackfillStore is what the backfill needs from the datastore.
type memoryEmbeddingBackfillStore interface {
	TryAcquireSchedulerLeaderLock(ctx context.Context, lockKey int64) (datastore.SchedulerLeaderLock, bool, error)
	BackfillMemoryEmbeddings(ctx context.Context, batchSize int, createEmbeddings datastore.MemoryImportBatchEmbeddingFunc) (datastore.MemoryEmbeddingBackfillStats, error)
}

// MemoryEmbeddingBackfillConfig configures StartMemoryEmbeddingBackfill.
type MemoryEmbeddingBackfillConfig struct {
	// LockKey is the Postgres advisory lock key that elects the one instance
	// running the pass (see server.Config.MemoryEmbeddingBackfillLockKey).
	LockKey int64
}

// StartMemoryEmbeddingBackfill embeds, in the background, every active
// non-Summary memory that has no Embedding row: rows written before memories
// were embedded on save (issue #248), and any whose post-save embedding failed.
// It runs once, at startup, and never again: new and edited memories are embedded
// when they are saved, so this only has to catch up the rows that predate that,
// plus any save-time failure since the last restart. The pass runs only on the
// instance that wins the advisory lock; the others skip it quietly, as does a
// database without advisory locks. Under a mock or local LLM backend it never
// starts: embeddings cannot reach a provider there (deny-network client), so the
// pass would only make failing calls.
func (a *Agent) StartMemoryEmbeddingBackfill(ctx context.Context, cfg MemoryEmbeddingBackfillConfig) {
	if run, reason := a.memoryEmbeddingBackfillEnabled(); !run {
		a.logger.Info("memory embedding backfill disabled", zap.String("reason", reason))
		return
	}
	go runMemoryEmbeddingBackfillPass(ctx, a.ds, cfg.LockKey, a.memoryTool.CreateEmbeddings, a.logger)
}

// memoryEmbeddingBackfillEnabled reports whether the backfill should run, and
// why not when it should not.
func (a *Agent) memoryEmbeddingBackfillEnabled() (bool, string) {
	switch {
	case a.nonVendorLLM():
		return false, "non-vendor LLM backend (mock/local): embeddings have no provider"
	case a.memoryTool == nil:
		return false, "memory tool is unavailable"
	default:
		return true, ""
	}
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
		// skip the pass; the next startup tries again.
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
