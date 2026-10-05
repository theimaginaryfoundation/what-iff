package agent

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeChatTurnStore is an in-memory chatTurnStore: it keeps turn jobs per chat and lists the
// pending/processing ones oldest first, as the datastore does.
type fakeChatTurnStore struct {
	mu        sync.Mutex
	jobs      map[uuid.UUID]*models.Job
	chatOf    map[uuid.UUID]uuid.UUID
	listErrs  []error // returned (and consumed) by the next List calls, in order
	listCalls int
	touches   map[uuid.UUID]int
	progress  map[uuid.UUID][]string // progress payloads written per job, in order
}

func newFakeChatTurnStore() *fakeChatTurnStore {
	return &fakeChatTurnStore{jobs: map[uuid.UUID]*models.Job{}, chatOf: map[uuid.UUID]uuid.UUID{}, touches: map[uuid.UUID]int{}, progress: map[uuid.UUID][]string{}}
}

// add registers a turn job in chatID created at createdAt (UpdatedAt too) and returns a copy.
func (f *fakeChatTurnStore) add(userID, chatID uuid.UUID, status models.JobStatus, createdAt time.Time) *models.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := &models.Job{ID: uuid.New(), UserID: userID, JobType: JobTypeChatMessage, Reference: uuid.NewString(),
		Status: status, CreatedAt: createdAt, UpdatedAt: createdAt}
	f.jobs[j.ID] = j
	f.chatOf[j.ID] = chatID
	cp := *j
	return &cp
}

func (f *fakeChatTurnStore) setStatus(id uuid.UUID, status models.JobStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[id].Status = status
	f.jobs[id].UpdatedAt = time.Now()
}

func (f *fakeChatTurnStore) status(id uuid.UUID) models.JobStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[id].Status
}

func (f *fakeChatTurnStore) touchCount(id uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.touches[id]
}

func (f *fakeChatTurnStore) ListPendingTurnJobsForChat(_ context.Context, userID, chatID, excludeJobID uuid.UUID) ([]*models.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if len(f.listErrs) > 0 {
		err := f.listErrs[0]
		f.listErrs = f.listErrs[1:]
		return nil, err
	}
	var out []*models.Job
	for id, j := range f.jobs {
		if id == excludeJobID || f.chatOf[id] != chatID || j.UserID != userID || !blocksLaterTurns(j.Status) {
			continue
		}
		cp := *j
		out = append(out, &cp)
	}
	slices.SortFunc(out, func(a, b *models.Job) int {
		if turnJobBefore(a.CreatedAt, a.ID, b.CreatedAt, b.ID) {
			return -1
		}
		return 1
	})
	return out, nil
}

func (f *fakeChatTurnStore) JobStatus(_ context.Context, _, id uuid.UUID) (models.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return "", errors.New("job not found")
	}
	return j.Status, nil
}

func (f *fakeChatTurnStore) TouchJob(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.jobs[id]; ok {
		j.UpdatedAt = time.Now()
	}
	f.touches[id]++
	return nil
}

func (f *fakeChatTurnStore) UpdateJobProgress(_ context.Context, _, id uuid.UUID, progress string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress[id] = append(f.progress[id], progress)
	return nil
}

func (f *fakeChatTurnStore) progressWrites(id uuid.UUID) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.progress[id])
}

func (f *fakeChatTurnStore) CreateJob(_ context.Context, userID uuid.UUID, job models.Job) (*models.Job, error) {
	chatID, err := uuid.Parse(job.Reference)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	job.ID = uuid.New()
	job.UserID = userID
	job.CreatedAt = time.Now()
	job.UpdatedAt = job.CreatedAt
	f.jobs[job.ID] = &job
	f.chatOf[job.ID] = chatID
	cp := job
	return &cp, nil
}

func (f *fakeChatTurnStore) UpdateJobStatus(_ context.Context, _, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return nil, errors.New("job not found")
	}
	j.Status = status
	j.Error = errorMsg
	j.UpdatedAt = time.Now()
	cp := *j
	return &cp, nil
}

// FinishTurnJobIfActive mirrors the datastore: a no-op on a terminal job, complete when the turn
// replied (per the caller, the row's result or its status), failed otherwise.
func (f *fakeChatTurnStore) FinishTurnJobIfActive(_ context.Context, _, id uuid.UUID, replied bool, failMsg string) (models.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok || isTerminalJobStatus(j.Status) {
		return "", nil
	}
	if replied || j.ResultID != nil || hasReplied(j.Status) {
		j.Status, j.Error = models.JobStatusComplete, ""
	} else {
		j.Status, j.Error = models.JobStatusFailed, failMsg
	}
	j.UpdatedAt = time.Now()
	return j.Status, nil
}

// testTurnGate builds a gate over store with fast polling, no heartbeat and the given timeout.
func testTurnGate(store chatTurnStore, timeout time.Duration) *chatTurnGate {
	return &chatTurnGate{
		store:       store,
		logger:      zap.NewNop(),
		tracker:     &chatTurnTracker{},
		timeout:     timeout,
		staleAfter:  chatTurnStaleAfter,
		pollInitial: 2 * time.Millisecond,
		pollMax:     10 * time.Millisecond,
	}
}

type waitResult struct {
	waited time.Duration
	err    error
}

func waitAsync(g *chatTurnGate, ctx context.Context, self *models.Job, chatID uuid.UUID) <-chan waitResult {
	done := make(chan waitResult, 1)
	go func() {
		waited, err := g.wait(ctx, self, chatID)
		done <- waitResult{waited, err}
	}()
	return done
}

func requireStillWaiting(t *testing.T, done <-chan waitResult) {
	t.Helper()
	select {
	case r := <-done:
		t.Fatalf("turn proceeded while an earlier turn was live: %+v", r)
	case <-time.After(50 * time.Millisecond):
	}
}

func requireProceeds(t *testing.T, done <-chan waitResult) waitResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("turn never proceeded")
		return waitResult{}
	}
}

func TestChatTurnGate_SecondTurnWaitsUntilFirstHasReplied(t *testing.T) {
	t.Parallel()
	// inference_complete is enough: the reply and response chain are saved by then, and the
	// earlier turn's post-processing overlaps the next turn.
	for _, released := range []models.JobStatus{models.JobStatusInferenceComplete, models.JobStatusComplete, models.JobStatusFailed, models.JobStatusCancelled} {
		t.Run(string(released), func(t *testing.T) {
			t.Parallel()
			store := newFakeChatTurnStore()
			userID, chatID := uuid.New(), uuid.New()
			now := time.Now()
			first := store.add(userID, chatID, models.JobStatusPending, now.Add(-time.Second))
			second := store.add(userID, chatID, models.JobStatusPending, now)
			g := testTurnGate(store, time.Minute)

			done := waitAsync(g, context.Background(), second, chatID)
			requireStillWaiting(t, done)
			store.setStatus(first.ID, models.JobStatusProcessing)
			requireStillWaiting(t, done)

			store.setStatus(first.ID, released)
			r := requireProceeds(t, done)
			require.NoError(t, r.err)
			require.Positive(t, r.waited)
		})
	}
}

func TestChatTurnGate_ProceedsAtOnceWithoutEarlierTurns(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatA, chatB := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	// A live turn in another chat, a NEWER live turn in the same chat, and an older turn already
	// past its reply never block.
	store.add(userID, chatB, models.JobStatusProcessing, now.Add(-time.Minute))
	store.add(userID, chatA, models.JobStatusCompactionComplete, now.Add(-time.Minute))
	self := store.add(userID, chatA, models.JobStatusPending, now.Add(-time.Second))
	store.add(userID, chatA, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	waited, err := g.wait(context.Background(), self, chatA)
	require.NoError(t, err)
	require.Zero(t, waited)
	require.Equal(t, 1, store.listCalls, "the common case is a single read")
}

func TestChatTurnGate_OtherChatsDoNotBlockEachOther(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatA, chatB := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	a := store.add(userID, chatA, models.JobStatusProcessing, now.Add(-time.Second))
	b := store.add(userID, chatB, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	_, err := g.wait(context.Background(), b, chatB)
	require.NoError(t, err)
	_, err = g.wait(context.Background(), a, chatA)
	require.NoError(t, err)
}

func TestChatTurnGate_StaleEarlierTurnDoesNotBlockButHeartbeatingOneDoes(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	// Left "processing" by an instance that died: no heartbeat for longer than the stale bound.
	dead := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-chatTurnStaleAfter-time.Minute))
	self := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	waited, err := g.wait(context.Background(), self, chatID)
	require.NoError(t, err)
	require.Zero(t, waited)

	// The same old job, still heartbeating, is live and blocks.
	require.NoError(t, store.TouchJob(context.Background(), userID, dead.ID))
	done := waitAsync(g, context.Background(), self, chatID)
	requireStillWaiting(t, done)
	store.setStatus(dead.ID, models.JobStatusInferenceComplete)
	require.NoError(t, requireProceeds(t, done).err)
}

func TestChatTurnGate_TimesOutWithTypedError(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	self := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, 40*time.Millisecond)

	waited, err := g.wait(context.Background(), self, chatID)
	require.ErrorIs(t, err, ErrChatTurnWaitTimeout)
	require.GreaterOrEqual(t, waited, 40*time.Millisecond)
}

func TestChatTurnGate_ListErrorsAreRetriedThenTimeOut(t *testing.T) {
	t.Parallel()
	userID, chatID := uuid.New(), uuid.New()
	listErr := errors.New("db hiccup")

	// A transient error is retried; the turn proceeds once the read succeeds.
	store := newFakeChatTurnStore()
	store.listErrs = []error{listErr, listErr}
	self := store.add(userID, chatID, models.JobStatusPending, time.Now())
	_, err := testTurnGate(store, time.Minute).wait(context.Background(), self, chatID)
	require.NoError(t, err)
	require.Equal(t, 3, store.listCalls)

	// A persistent one fails closed at the deadline, with the last error attached.
	store = newFakeChatTurnStore()
	store.listErrs = slices.Repeat([]error{listErr}, 1000)
	self = store.add(userID, chatID, models.JobStatusPending, time.Now())
	_, err = testTurnGate(store, 30*time.Millisecond).wait(context.Background(), self, chatID)
	require.ErrorIs(t, err, ErrChatTurnWaitTimeout)
	require.ErrorIs(t, err, listErr)
}

func TestChatTurnGate_CancelWhileQueued(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	self := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	done := waitAsync(g, ctx, self, chatID)
	requireStillWaiting(t, done)
	cancel()
	r := requireProceeds(t, done)
	require.ErrorIs(t, r.err, context.Canceled)
}

// A queued job cancelled in the database (Stop handled by another instance) does not run once the
// gate opens.
func TestChatTurnGate_JobCancelledWhileQueuedDoesNotRun(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	self := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	type result struct {
		release func()
		err     error
	}
	done := make(chan result, 1)
	go func() {
		release, err := g.acquire(context.Background(), self, chatID, nil)
		done <- result{release, err}
	}()
	time.Sleep(30 * time.Millisecond)
	store.setStatus(self.ID, models.JobStatusCancelled)
	store.setStatus(first.ID, models.JobStatusInferenceComplete)

	select {
	case r := <-done:
		require.ErrorIs(t, r.err, errQueuedTurnCancelled)
		require.ErrorIs(t, r.err, context.Canceled)
		require.Nil(t, r.release)
	case <-time.After(2 * time.Second):
		t.Fatal("gate never opened")
	}
	require.Empty(t, g.tracker.snapshot(), "a turn that never started is no longer tracked")
}

func TestChatTurnGate_WakeIsPerChat(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatA, chatB := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatA, models.JobStatusProcessing, now.Add(-time.Second))
	second := store.add(userID, chatA, models.JobStatusPending, now)
	g := testTurnGate(store, time.Hour)
	g.pollInitial, g.pollMax = time.Hour, time.Hour // only the in-process signal can wake it

	done := waitAsync(g, context.Background(), second, chatA)
	requireStillWaiting(t, done)
	store.setStatus(first.ID, models.JobStatusInferenceComplete)
	g.tracker.wakeChat(chatB)
	requireStillWaiting(t, done)
	g.tracker.wakeChat(chatA)
	require.NoError(t, requireProceeds(t, done).err)
}

// A turn reaching inference_complete in this process (noteTurnJobStatus, called from the job phase
// helpers) wakes the turns queued behind it at once.
func TestNoteTurnJobStatus_WakesQueuedTurnAtInferenceComplete(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	a := &Agent{logger: zap.NewNop()}
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	second := store.add(userID, chatID, models.JobStatusPending, now)
	a.turns.add(first.ID, userID, chatID)
	g := testTurnGate(store, time.Hour)
	g.tracker = &a.turns
	g.pollInitial, g.pollMax = time.Hour, time.Hour

	done := waitAsync(g, context.Background(), second, chatID)
	requireStillWaiting(t, done)
	a.noteTurnJobStatus(&models.Job{ID: first.ID, Status: models.JobStatusProcessing})
	requireStillWaiting(t, done)

	store.setStatus(first.ID, models.JobStatusInferenceComplete)
	a.noteTurnJobStatus(&models.Job{ID: first.ID, Status: models.JobStatusInferenceComplete})
	require.NoError(t, requireProceeds(t, done).err)
	require.True(t, a.turns.snapshot()[first.ID].replied)
}

func TestChatTurnGate_HeartbeatsWhileQueuedAndRunning(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	self := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)
	g.heartbeatInterval = 5 * time.Millisecond

	acquired := make(chan func(), 1)
	go func() {
		release, err := g.acquire(context.Background(), self, chatID, nil)
		if err == nil {
			acquired <- release
		}
	}()
	require.Eventually(t, func() bool { return store.touchCount(self.ID) > 0 }, 2*time.Second, 5*time.Millisecond,
		"a queued turn heartbeats")
	store.setStatus(first.ID, models.JobStatusInferenceComplete)
	release := <-acquired
	before := store.touchCount(self.ID)
	require.Eventually(t, func() bool { return store.touchCount(self.ID) > before }, 2*time.Second, 5*time.Millisecond,
		"a running turn heartbeats")
	release()
	time.Sleep(20 * time.Millisecond)
	stopped := store.touchCount(self.ID)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, stopped, store.touchCount(self.ID), "release stops the heartbeat")
}

func TestBlockingTurnJobs_OrderAndFilters(t *testing.T) {
	t.Parallel()
	g := testTurnGate(nil, time.Minute)
	now := time.Now()
	at := now.Add(-time.Minute).Truncate(time.Microsecond)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	slices.SortFunc(ids, func(a, b uuid.UUID) int {
		if a.String() < b.String() {
			return -1
		}
		return 1
	})
	lowID, highID := ids[0], ids[1]

	// self's in-memory created_at carries nanoseconds the database rounds away.
	self := &models.Job{ID: highID, CreatedAt: at.Add(300 * time.Nanosecond), UpdatedAt: at, Status: models.JobStatusPending}
	older := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Second), UpdatedAt: now, Status: models.JobStatusProcessing}
	tieLow := &models.Job{ID: lowID, CreatedAt: at, UpdatedAt: now, Status: models.JobStatusPending}
	newer := &models.Job{ID: uuid.New(), CreatedAt: at.Add(time.Second), UpdatedAt: now, Status: models.JobStatusProcessing}
	olderReplied := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Hour), UpdatedAt: now, Status: models.JobStatusInferenceComplete}
	olderFailed := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Hour), UpdatedAt: now, Status: models.JobStatusFailed}
	olderStale := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Hour), UpdatedAt: now.Add(-chatTurnStaleAfter - time.Second), Status: models.JobStatusProcessing}

	got := g.blockingTurnJobs([]*models.Job{olderStale, olderFailed, olderReplied, older, tieLow, self, newer}, self, now)
	require.Equal(t, []*models.Job{older, tieLow}, got,
		"older live jobs before their reply block (ties by id); replied, terminal, stale and newer ones don't")
}

// TestAwaitChatTurn_SerializesTurnsInJobOrder runs two turns on one chat through the Agent's gate,
// starting the newer one first: it runs only after the older turn has replied.
func TestAwaitChatTurn_SerializesTurnsInJobOrder(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	a := &Agent{logger: zap.NewNop()}
	a.testHooks.ChatTurnStore = store
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatID, models.JobStatusPending, now.Add(-time.Millisecond))
	second := store.add(userID, chatID, models.JobStatusPending, now)

	var mu sync.Mutex
	var order []string
	runTurn := func(job *models.Job, name string, work time.Duration) error {
		release, err := a.awaitChatTurn(context.Background(), job, chatID)
		if err != nil {
			return err
		}
		defer release()
		mu.Lock()
		order = append(order, name+":start")
		mu.Unlock()
		time.Sleep(work)
		mu.Lock()
		order = append(order, name+":replied")
		mu.Unlock()
		store.setStatus(job.ID, models.JobStatusInferenceComplete)
		a.noteTurnJobStatus(&models.Job{ID: job.ID, Status: models.JobStatusInferenceComplete})
		return nil
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[1] = runTurn(second, "second", 0) }()
	time.Sleep(20 * time.Millisecond)
	go func() { defer wg.Done(); errs[0] = runTurn(first, "first", 50*time.Millisecond) }()
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	require.Equal(t, []string{"first:start", "first:replied", "second:start", "second:replied"}, order)
	require.Empty(t, a.turns.snapshot(), "released turns are no longer tracked")
}

func TestAwaitChatTurn_NoStoreIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	release, err := a.awaitChatTurn(context.Background(), &models.Job{ID: uuid.New()}, uuid.New())
	require.NoError(t, err)
	release()
}

func TestAwaitUserChatTurn_ReleaseFinishesAJobLeftNonTerminal(t *testing.T) {
	t.Parallel()
	resultID := uuid.New()
	for name, tc := range map[string]struct {
		dbStatus models.JobStatus
		resultID *uuid.UUID
		want     models.JobStatus
	}{
		"reply saved, final write lost": {models.JobStatusCompactionComplete, &resultID, models.JobStatusComplete},
		"no reply":                      {models.JobStatusProcessing, nil, models.JobStatusFailed},
		"cancelled elsewhere is kept":   {models.JobStatusCancelled, nil, models.JobStatusCancelled},
		"reply saved per the row only":  {models.JobStatusInferenceComplete, nil, models.JobStatusComplete},
		"completed elsewhere is kept":   {models.JobStatusComplete, nil, models.JobStatusComplete},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newFakeChatTurnStore()
			a := &Agent{logger: zap.NewNop()}
			a.testHooks.ChatTurnStore = store
			userID, chatID := uuid.New(), uuid.New()
			job := store.add(userID, chatID, models.JobStatusProcessing, time.Now())

			release, err := a.awaitUserChatTurn(context.Background(), job, chatID)
			require.NoError(t, err)
			store.setStatus(job.ID, tc.dbStatus)
			job.Status = models.JobStatusCompactionComplete // the worker's last successful write
			job.ResultID = tc.resultID
			release()
			require.Equal(t, tc.want, store.status(job.ID))
		})
	}
}

func TestFailInFlightTurns_FinishesThisProcesssTurnJobs(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	a := &Agent{logger: zap.NewNop()}
	a.testHooks.ChatTurnStore = store
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	running := store.add(userID, chatID, models.JobStatusProcessing, now)
	replied := store.add(userID, chatID, models.JobStatusCompactionComplete, now)
	stopped := store.add(userID, chatID, models.JobStatusCancelled, now)
	elsewhere := store.add(userID, chatID, models.JobStatusProcessing, now) // another instance's
	for _, j := range []*models.Job{running, replied, stopped} {
		a.turns.add(j.ID, userID, chatID)
	}

	a.FailInFlightTurns(context.Background())
	require.Equal(t, models.JobStatusFailed, store.status(running.ID))
	require.Equal(t, models.JobStatusComplete, store.status(replied.ID), "a turn past its reply completes")
	require.Equal(t, models.JobStatusCancelled, store.status(stopped.ID), "terminal jobs are left alone")
	require.Equal(t, models.JobStatusProcessing, store.status(elsewhere.ID), "only this process's turns")
}

func TestBeginEphemeralChatTurn_UntrackedTurnHoldsATicket(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		outcome error
		want    models.JobStatus
	}{
		"complete":  {nil, models.JobStatusComplete},
		"failed":    {errors.New("provider down"), models.JobStatusFailed},
		"cancelled": {context.Canceled, models.JobStatusCancelled},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newFakeChatTurnStore()
			a := &Agent{logger: zap.NewNop()}
			a.testHooks.ChatTurnStore = store
			userID, chatID := uuid.New(), uuid.New()

			end, err := a.beginEphemeralChatTurn(context.Background(), userID, chatID, nil)
			require.NoError(t, err)
			live, err := store.ListPendingTurnJobsForChat(context.Background(), userID, chatID, uuid.Nil)
			require.NoError(t, err)
			require.Len(t, live, 1, "the ticket holds the turn's place in the chat")
			ticket := live[0]
			require.Equal(t, JobTypeAgentJobRun, ticket.JobType)
			require.Equal(t, chatID.String(), ticket.Reference)
			require.Equal(t, models.JobStatusProcessing, ticket.Status)

			end(tc.outcome)
			require.Equal(t, tc.want, store.status(ticket.ID))
		})
	}
}

func TestBeginEphemeralChatTurn_UntrackedTurnQueuesBehindLiveTurn(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	a := &Agent{logger: zap.NewNop()}
	a.testHooks.ChatTurnStore = store
	userID, chatID := uuid.New(), uuid.New()
	running := store.add(userID, chatID, models.JobStatusProcessing, time.Now().Add(-time.Second))

	started := make(chan func(error), 1)
	go func() {
		end, err := a.beginEphemeralChatTurn(context.Background(), userID, chatID, nil)
		if err == nil {
			started <- end
		}
	}()
	select {
	case <-started:
		t.Fatal("scheduled turn started while a turn was running in its chat")
	case <-time.After(50 * time.Millisecond):
	}
	store.setStatus(running.ID, models.JobStatusInferenceComplete)
	a.turns.wakeChat(chatID)
	select {
	case end := <-started:
		end(nil)
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled turn never started")
	}
}

func TestBeginEphemeralChatTurn_TrackedTurnQueuesAsItsOwnJob(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	a := &Agent{logger: zap.NewNop()}
	a.testHooks.ChatTurnStore = store
	userID, chatID := uuid.New(), uuid.New()
	tracking := store.add(userID, chatID, models.JobStatusProcessing, time.Now())

	end, err := a.beginEphemeralChatTurn(context.Background(), userID, chatID, tracking)
	require.NoError(t, err)
	end(errors.New("ignored: the caller owns the tracking job's status"))
	require.Equal(t, models.JobStatusProcessing, store.status(tracking.ID))
	live, err := store.ListPendingTurnJobsForChat(context.Background(), userID, chatID, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, live, 1, "no ticket is created for a tracked turn")
}

func TestChatTurnGate_ShowsWhatTheQueuedTurnWaitsOn(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	second := store.add(userID, chatID, models.JobStatusProcessing, now)
	g := testTurnGate(store, 5*time.Second)

	done := waitAsync(g, context.Background(), second, chatID)
	// Let the earlier turn go only once the gate has written what it waits on, so the test does
	// not depend on poll timing, and keep it waiting through several polls to see it written once.
	require.Eventually(t, func() bool { return len(store.progressWrites(second.ID)) >= 1 }, 2*time.Second, time.Millisecond)
	requireStillWaiting(t, done)
	store.setStatus(first.ID, models.JobStatusInferenceComplete)
	requireProceeds(t, done)

	// Written once while queued, and cleared when the turn goes.
	require.Equal(t, []string{
		`{"tool_calls":[],"waiting_on":"reply"}`,
		`{"tool_calls":[]}`,
	}, store.progressWrites(second.ID))
}

func TestChatTurnGate_WritesNoProgressWhenNotQueued(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	job := store.add(uuid.New(), uuid.New(), models.JobStatusProcessing, time.Now())
	_, err := testTurnGate(store, time.Second).wait(context.Background(), job, store.chatOf[job.ID])
	require.NoError(t, err)
	require.Empty(t, store.progressWrites(job.ID))
}
