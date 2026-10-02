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
// non-terminal ones oldest first, as the datastore does.
type fakeChatTurnStore struct {
	mu        sync.Mutex
	jobs      map[uuid.UUID]*models.Job
	chatOf    map[uuid.UUID]uuid.UUID
	listErrs  []error // returned (and consumed) by the next List calls, in order
	listCalls int
}

func newFakeChatTurnStore() *fakeChatTurnStore {
	return &fakeChatTurnStore{jobs: map[uuid.UUID]*models.Job{}, chatOf: map[uuid.UUID]uuid.UUID{}}
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

func (f *fakeChatTurnStore) ListActiveTurnJobsForChat(_ context.Context, userID, chatID uuid.UUID) ([]*models.Job, error) {
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
		if f.chatOf[id] != chatID || j.UserID != userID || isTerminalJobStatus(j.Status) {
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

// testTurnGate builds a gate over store with fast polling and the given timeout.
func testTurnGate(store chatTurnStore, timeout time.Duration) *chatTurnGate {
	return &chatTurnGate{
		store:       store,
		logger:      zap.NewNop(),
		signal:      &turnSignal{},
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

func TestChatTurnGate_SecondTurnWaitsUntilFirstIsTerminal(t *testing.T) {
	t.Parallel()
	for _, terminal := range []models.JobStatus{models.JobStatusComplete, models.JobStatusFailed, models.JobStatusCancelled} {
		t.Run(string(terminal), func(t *testing.T) {
			t.Parallel()
			store := newFakeChatTurnStore()
			userID, chatID := uuid.New(), uuid.New()
			now := time.Now()
			first := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
			second := store.add(userID, chatID, models.JobStatusPending, now)
			g := testTurnGate(store, time.Minute)

			done := waitAsync(g, context.Background(), second, chatID)
			requireStillWaiting(t, done)

			// Post-inference phases are still non-terminal: the second turn keeps waiting for the
			// first one's checkpoint/scratchpad writes too.
			store.setStatus(first.ID, models.JobStatusInferenceComplete)
			requireStillWaiting(t, done)

			store.setStatus(first.ID, terminal)
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
	// A live turn in another chat, and a NEWER live turn in the same chat, never block.
	store.add(userID, chatB, models.JobStatusProcessing, now.Add(-time.Minute))
	self := store.add(userID, chatA, models.JobStatusPending, now.Add(-time.Second))
	store.add(userID, chatA, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	waited, err := g.wait(context.Background(), self, chatA)
	require.NoError(t, err)
	require.Zero(t, waited)
	require.Equal(t, 1, store.listCalls)
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

func TestChatTurnGate_StaleEarlierTurnDoesNotBlock(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	// Left "processing" by an instance that died: not touched for longer than the stale cutoff.
	store.add(userID, chatID, models.JobStatusProcessing, now.Add(-chatTurnStaleAfter-time.Minute))
	self := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, time.Minute)

	waited, err := g.wait(context.Background(), self, chatID)
	require.NoError(t, err)
	require.Zero(t, waited)
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

func TestChatTurnGate_LocalTurnEndWakesWaiterWithoutPolling(t *testing.T) {
	t.Parallel()
	store := newFakeChatTurnStore()
	userID, chatID := uuid.New(), uuid.New()
	now := time.Now()
	first := store.add(userID, chatID, models.JobStatusProcessing, now.Add(-time.Second))
	second := store.add(userID, chatID, models.JobStatusPending, now)
	g := testTurnGate(store, time.Hour)
	g.pollInitial, g.pollMax = time.Hour, time.Hour // only the in-process signal can wake it

	done := waitAsync(g, context.Background(), second, chatID)
	requireStillWaiting(t, done)
	store.setStatus(first.ID, models.JobStatusComplete)
	g.signal.broadcast()
	r := requireProceeds(t, done)
	require.NoError(t, r.err)
}

func TestBlockingTurnJobs_OrderAndFilters(t *testing.T) {
	t.Parallel()
	g := testTurnGate(nil, time.Minute)
	now := time.Now()
	at := now.Add(-time.Minute)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	slices.SortFunc(ids, func(a, b uuid.UUID) int {
		if a.String() < b.String() {
			return -1
		}
		return 1
	})
	lowID, highID := ids[0], ids[1]

	self := &models.Job{ID: highID, CreatedAt: at, UpdatedAt: at, Status: models.JobStatusPending}
	older := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Second), UpdatedAt: at, Status: models.JobStatusProcessing}
	tieLow := &models.Job{ID: lowID, CreatedAt: at, UpdatedAt: at, Status: models.JobStatusPending}
	newer := &models.Job{ID: uuid.New(), CreatedAt: at.Add(time.Second), UpdatedAt: at, Status: models.JobStatusProcessing}
	olderFailed := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Hour), UpdatedAt: at, Status: models.JobStatusFailed}
	olderCancelled := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Hour), UpdatedAt: at, Status: models.JobStatusCancelled}
	olderStale := &models.Job{ID: uuid.New(), CreatedAt: at.Add(-time.Hour), UpdatedAt: now.Add(-chatTurnStaleAfter - time.Second), Status: models.JobStatusProcessing}

	got := g.blockingTurnJobs([]*models.Job{olderStale, olderFailed, olderCancelled, older, tieLow, self, newer}, self, now)
	require.Equal(t, []*models.Job{older, tieLow}, got, "older live jobs block (ties by id); terminal, stale and newer ones don't")

	// The stored created_at of self (database precision) wins over the in-memory one.
	selfInMemory := &models.Job{ID: highID, CreatedAt: at.Add(-time.Hour), Status: models.JobStatusPending}
	got = g.blockingTurnJobs([]*models.Job{older, self}, selfInMemory, now)
	require.Equal(t, []*models.Job{older}, got)
}

// TestAwaitChatTurn_SerializesTurnsInJobOrder runs two turns on one chat through the Agent's gate,
// starting the newer one first: it runs only after the older turn is terminal and released.
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
		order = append(order, name+":end")
		mu.Unlock()
		store.setStatus(job.ID, models.JobStatusComplete)
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
	require.Equal(t, []string{"first:start", "first:end", "second:start", "second:end"}, order)
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

func TestAwaitChatTurn_NoStoreIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	release, err := a.awaitChatTurn(context.Background(), &models.Job{ID: uuid.New()}, uuid.New())
	require.NoError(t, err)
	release()
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
			live, err := store.ListActiveTurnJobsForChat(context.Background(), userID, chatID)
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
	store.setStatus(running.ID, models.JobStatusComplete)
	a.turnEnded.broadcast()
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
	live, err := store.ListActiveTurnJobsForChat(context.Background(), userID, chatID)
	require.NoError(t, err)
	require.Len(t, live, 1, "no ticket is created for a tracked turn")
}
