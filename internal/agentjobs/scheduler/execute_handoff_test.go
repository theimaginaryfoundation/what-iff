package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// blockingJobStore blocks every execution in GetAgentJob until released, standing in for a run
// that is queued behind a busy chat turn.
type blockingJobStore struct {
	fakeDatastoreProvider
	entered chan struct{}
	release chan struct{}
}

func (s *blockingJobStore) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	s.entered <- struct{}{}
	<-s.release
	return s.fakeDatastoreProvider.GetAgentJob(ctx, userID, id)
}

// TestExecute_HandsRunOffWithoutHoldingTheWorker locks in that a quartz worker is not held for the
// length of a run: Execute returns while the run is still blocked, so a run waiting on a busy chat
// cannot starve the scheduler's few workers.
func TestExecute_HandsRunOffWithoutHoldingTheWorker(t *testing.T) {
	store := &blockingJobStore{entered: make(chan struct{}, 2), release: make(chan struct{})}
	m := &Manager{ds: store, logger: zap.NewNop(), inFlight: map[uuid.UUID]bool{}, fingerprints: map[uuid.UUID]string{}}

	for range 2 {
		job := &agentJobQuartzJob{manager: m, userID: uuid.New(), agentJobID: uuid.New()}
		ctx, cancel := context.WithCancel(context.Background())
		returned := make(chan struct{})
		go func() {
			require.NoError(t, job.Execute(ctx))
			close(returned)
		}()
		select {
		case <-returned:
		case <-time.After(2 * time.Second):
			t.Fatal("Execute held the worker for the whole run")
		}
		cancel() // the worker's context ending must not cut the handed-off run short
		select {
		case <-store.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("the run never started")
		}
	}
	close(store.release)
}

// panickingJobStore panics in GetAgentJob, standing in for a run that panics mid-turn.
type panickingJobStore struct {
	fakeDatastoreProvider
}

func (s *panickingJobStore) GetAgentJob(context.Context, uuid.UUID, uuid.UUID) (*models.AgentJob, error) {
	panic("boom")
}

// A detached run that panics is recovered (quartz no longer runs it, so nothing else would) and
// clears its overlap guard, so the same agent job can run again.
func TestExecuteAgentJobDetached_RecoversAPanic(t *testing.T) {
	m := &Manager{ds: &panickingJobStore{}, logger: zap.NewNop(), inFlight: map[uuid.UUID]bool{}, fingerprints: map[uuid.UUID]string{}}
	agentJobID := uuid.New()

	require.NotPanics(t, func() {
		m.executeAgentJobDetached(context.Background(), uuid.New(), agentJobID, executionOptions{})
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	require.False(t, m.inFlight[agentJobID])
}
