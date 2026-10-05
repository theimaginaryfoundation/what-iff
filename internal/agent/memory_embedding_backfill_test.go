package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

type fakeBackfillLock struct{ released bool }

func (l *fakeBackfillLock) IsHealthy(context.Context) (bool, error) { return true, nil }
func (l *fakeBackfillLock) Release(context.Context) error {
	l.released = true
	return nil
}
func (l *fakeBackfillLock) Key() int64 { return 1 }

type fakeBackfillStore struct {
	lock       *fakeBackfillLock
	acquired   bool
	lockErr    error
	lockKey    int64
	backfills  int
	backfillFn func(ctx context.Context) error
}

func (f *fakeBackfillStore) TryAcquireSchedulerLeaderLock(_ context.Context, key int64) (datastore.SchedulerLeaderLock, bool, error) {
	f.lockKey = key
	if f.lockErr != nil {
		return nil, false, f.lockErr
	}
	if !f.acquired {
		return nil, false, nil
	}
	return f.lock, true, nil
}

func (f *fakeBackfillStore) BackfillMemoryEmbeddings(ctx context.Context, _ int, _ datastore.MemoryImportBatchEmbeddingFunc) (datastore.MemoryEmbeddingBackfillStats, error) {
	f.backfills++
	if f.backfillFn != nil {
		return datastore.MemoryEmbeddingBackfillStats{}, f.backfillFn(ctx)
	}
	return datastore.MemoryEmbeddingBackfillStats{Embedded: 1}, nil
}

func TestMemoryEmbeddingBackfillEnabled_SkipsNonVendorBackends(t *testing.T) {
	memoryTool := tools.NewVectorStoreMemoryTool(nil, nil, zap.NewNop())

	tests := []struct {
		name  string
		agent *Agent
		want  bool
	}{
		{name: "vendor backend", agent: &Agent{memoryTool: memoryTool}, want: true},
		{name: "mock backend", agent: &Agent{memoryTool: memoryTool, mockLLM: true}, want: false},
		{name: "local backend", agent: &Agent{memoryTool: memoryTool, localLLM: true}, want: false},
		{name: "no memory tool", agent: &Agent{}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := tc.agent.memoryEmbeddingBackfillEnabled()
			require.Equal(t, tc.want, got)
			if !tc.want {
				require.NotEmpty(t, reason)
			}
		})
	}
}

func noopEmbeddings(context.Context, []string) ([][]float32, error) { return nil, nil }

func TestMemoryEmbeddingBackfillPass_RunsOnlyUnderTheLock(t *testing.T) {
	ctx := context.Background()

	t.Run("lock acquired: runs and releases", func(t *testing.T) {
		store := &fakeBackfillStore{lock: &fakeBackfillLock{}, acquired: true}
		ran := runMemoryEmbeddingBackfillPass(ctx, store, 80920033, noopEmbeddings, zap.NewNop())
		require.True(t, ran)
		require.Equal(t, int64(80920033), store.lockKey)
		require.Equal(t, 1, store.backfills)
		require.True(t, store.lock.released)
	})

	t.Run("lock held elsewhere: skips", func(t *testing.T) {
		store := &fakeBackfillStore{acquired: false}
		require.False(t, runMemoryEmbeddingBackfillPass(ctx, store, 1, noopEmbeddings, zap.NewNop()))
		require.Zero(t, store.backfills)
	})

	t.Run("no advisory locks: skips", func(t *testing.T) {
		store := &fakeBackfillStore{lockErr: errors.New("no such function: pg_try_advisory_lock")}
		require.False(t, runMemoryEmbeddingBackfillPass(ctx, store, 1, noopEmbeddings, zap.NewNop()))
		require.Zero(t, store.backfills)
	})

	t.Run("backfill error still releases the lock", func(t *testing.T) {
		store := &fakeBackfillStore{
			lock:       &fakeBackfillLock{},
			acquired:   true,
			backfillFn: func(context.Context) error { return errors.New("provider unavailable") },
		}
		require.True(t, runMemoryEmbeddingBackfillPass(ctx, store, 1, noopEmbeddings, zap.NewNop()))
		require.True(t, store.lock.released)
	})

	t.Run("cancelled context: does nothing", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		store := &fakeBackfillStore{lock: &fakeBackfillLock{}, acquired: true}
		require.False(t, runMemoryEmbeddingBackfillPass(cancelled, store, 1, noopEmbeddings, zap.NewNop()))
		require.Zero(t, store.backfills)
	})
}
