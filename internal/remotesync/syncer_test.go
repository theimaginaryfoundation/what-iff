package remotesync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// fakeRemote serves threads newest activity first and messages newest first, PageSize at a time.
type fakeRemote struct {
	mu       sync.Mutex
	active   []RemoteChat
	archived []RemoteChat
	messages map[uuid.UUID][]models.WebhookMessage // oldest first
	listErr  error
	// listPanic, when set, makes ListChats panic with it.
	listPanic any
	msgErr    map[uuid.UUID]error

	listCalls int
	msgCalls  map[uuid.UUID]int

	summaries   map[uuid.UUID]*models.ChatSummary
	summaryErr  error
	summaryHits int
}

func newFakeRemote() *fakeRemote {
	return &fakeRemote{messages: map[uuid.UUID][]models.WebhookMessage{}, msgErr: map[uuid.UUID]error{}, msgCalls: map[uuid.UUID]int{}}
}

func (r *fakeRemote) Host() string { return "remote.test" }

func (r *fakeRemote) GetSummary(_ context.Context, chatID uuid.UUID) (*models.ChatSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.summaryHits++
	if r.summaryErr != nil {
		return nil, r.summaryErr
	}
	return r.summaries[chatID], nil
}

// addThread adds an active thread with n messages a minute apart from start, and returns its id.
func (r *fakeRemote) addThread(name string, n int, start time.Time) uuid.UUID {
	id := uuid.New()
	var last *time.Time
	for i := range n {
		origin := models.MessageOriginUser
		if i%2 == 1 {
			origin = models.MessageOriginAssistant
		}
		at := start.Add(time.Duration(i) * time.Minute)
		r.messages[id] = append(r.messages[id], models.WebhookMessage{ID: uuid.New(), ChatID: id, Message: name + "-" + string(rune('a'+i%26)), Origin: origin, SentAt: at})
		last = &at
	}
	// Newest activity first: prepend.
	r.active = append([]RemoteChat{{ID: id, Name: name, LastMessageTime: last, CreatedAt: start}}, r.active...)
	return id
}

func (r *fakeRemote) ListChats(_ context.Context, page int, archived bool) ([]RemoteChat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	if r.listPanic != nil {
		panic(r.listPanic)
	}
	if r.listErr != nil {
		return nil, r.listErr
	}
	src := r.active
	if archived {
		src = r.archived
	}
	from := (page - 1) * PageSize
	if from >= len(src) {
		return nil, nil
	}
	return src[from:min(from+PageSize, len(src))], nil
}

func (r *fakeRemote) ListMessages(_ context.Context, chatID uuid.UUID, cursor string) ([]models.WebhookMessage, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgCalls[chatID]++
	if err := r.msgErr[chatID]; err != nil {
		return nil, "", err
	}
	all := r.messages[chatID]
	end := len(all)
	if cursor != "" {
		_, id, err := models.DecodeMessageCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		for i, m := range all {
			if m.ID == id {
				end = i
			}
		}
	}
	start := max(0, end-PageSize)
	page := make([]models.WebhookMessage, 0, end-start)
	for i := end - 1; i >= start; i-- {
		page = append(page, all[i])
	}
	next := ""
	if len(page) > 0 {
		oldest := page[len(page)-1]
		next = models.EncodeMessageCursor(oldest.SentAt, oldest.ID)
	}
	return page, next, nil
}

// fakeStore records what the syncer asks to apply and plays back links.
type fakeStore struct {
	mu      sync.Mutex
	account models.RemoteSyncAccount
	links   map[uuid.UUID]models.RemoteSyncChatLink
	applied map[uuid.UUID][][]models.RemoteSyncMessage
	// summaries is the Summary each apply carried, in order.
	summaries []*models.ChatSummary
	applyErr  map[uuid.UUID]error
	leased    bool
	finished  []models.RemoteSyncRunResult
	// rematches counts persona rematch calls; rematchN is what each reports.
	rematches int
	rematchN  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		account:  models.RemoteSyncAccount{UserID: uuid.New(), BaseURL: "https://remote.test", Token: "wht_x", Enabled: true},
		links:    map[uuid.UUID]models.RemoteSyncChatLink{},
		applied:  map[uuid.UUID][][]models.RemoteSyncMessage{},
		applyErr: map[uuid.UUID]error{},
	}
}

func (s *fakeStore) GetRemoteSyncAccount(context.Context, uuid.UUID) (*models.RemoteSyncAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.account
	return &a, nil
}

func (s *fakeStore) ListRemoteSyncChatLinks(context.Context, uuid.UUID) (map[uuid.UUID]models.RemoteSyncChatLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uuid.UUID]models.RemoteSyncChatLink, len(s.links))
	for k, v := range s.links {
		out[k] = v
	}
	return out, nil
}

// ApplyRemoteSyncThread records the batch and moves the link the way the datastore does.
func (s *fakeStore) ApplyRemoteSyncThread(_ context.Context, _ uuid.UUID, th models.RemoteSyncThread, batch []models.RemoteSyncMessage) (*models.RemoteSyncApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyErr[th.RemoteChatID]; err != nil {
		return nil, err
	}
	s.applied[th.RemoteChatID] = append(s.applied[th.RemoteChatID], batch)
	s.summaries = append(s.summaries, th.Summary)
	link, existed := s.links[th.RemoteChatID]
	link.RemoteChatID = th.RemoteChatID
	link.RemoteLastMessageTime = th.LastMessageTime
	if link.State == "" {
		link.State = models.RemoteSyncLinkActive
	}
	if n := len(batch); n > 0 {
		head := batch[n-1].SentAt
		link.RemoteHeadSentAt = &head
	}
	s.links[th.RemoteChatID] = link
	outcome := models.RemoteSyncAppended
	if !existed {
		outcome = models.RemoteSyncCreated
	}
	if link.State == models.RemoteSyncLinkDetached {
		outcome = models.RemoteSyncDetached
	}
	return &models.RemoteSyncApplyResult{Outcome: outcome, MessagesAdded: len(batch)}, nil
}

func (s *fakeStore) ClaimRemoteSync(context.Context, uuid.UUID, time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leased {
		return false, nil
	}
	s.leased = true
	return true, nil
}

func (s *fakeStore) ExtendRemoteSyncLease(context.Context, uuid.UUID, time.Time) error { return nil }

func (s *fakeStore) FinishRemoteSyncRun(_ context.Context, _ uuid.UUID, r models.RemoteSyncRunResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leased = false
	s.finished = append(s.finished, r)
	s.account.FullWalk = r.Status != models.RemoteSyncStatusOK || r.Stats.ThreadsDeferred > 0
	return nil
}

func (s *fakeStore) ReleaseRemoteSync(context.Context, uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leased = false
	return nil
}

func (s *fakeStore) RematchRemoteSyncPersonas(context.Context, uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rematches++
	return s.rematchN, nil
}

func (s *fakeStore) ListDueRemoteSyncs(context.Context, time.Time) ([]models.RemoteSyncDue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []models.RemoteSyncDue{{UserID: s.account.UserID, LastRunAt: s.account.LastRunAt}}, nil
}

func (s *fakeStore) last() models.RemoteSyncRunResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished[len(s.finished)-1]
}

func newTestSyncer(store *fakeStore, remote *fakeRemote, cfg Config) *Syncer {
	cfg.NewRemote = func(string, string) (Remote, error) { return remote, nil }
	return NewSyncer(store, cfg, nil)
}

func texts(batch []models.RemoteSyncMessage) []string {
	out := make([]string, len(batch))
	for i, m := range batch {
		out[i] = m.Message
	}
	return out
}

func TestFirstRunCopiesWholeHistoriesOldestFirst(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	store.account.FullWalk = true
	long := remote.addThread("long", 250, t0)
	short := remote.addThread("short", 3, t0.Add(time.Hour*10))
	empty := uuid.New()
	remote.active = append([]RemoteChat{{ID: empty, Name: "empty"}}, remote.active...)

	s := newTestSyncer(store, remote, Config{})
	s.RunDue(context.Background())

	res := store.last()
	assert.Equal(t, models.RemoteSyncStatusOK, res.Status)
	assert.Equal(t, 2, res.Stats.ThreadsCreated)
	assert.Equal(t, 253, res.Stats.MessagesAdded)
	require.Len(t, store.applied[long], 1)
	batch := store.applied[long][0]
	require.Len(t, batch, 250, "three pages walked")
	assert.True(t, batch[0].SentAt.Equal(t0), "oldest first")
	assert.Equal(t, []string{"short-a", "short-b", "short-c"}, texts(store.applied[short][0]))
	assert.NotContains(t, store.applied, empty, "threads without messages wait")
}

func TestLaterRunsPullOnlyNewMessagesWithAnOverlap(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	id := remote.addThread("trip", 150, t0)
	s := newTestSyncer(store, remote, Config{})
	s.RunDue(context.Background())

	// Two new remote messages.
	for i := range 2 {
		at := t0.Add(time.Duration(150+i) * time.Minute)
		remote.messages[id] = append(remote.messages[id], models.WebhookMessage{ID: uuid.New(), ChatID: id, Message: "new", Origin: models.MessageOriginUser, SentAt: at})
		remote.active[0].LastMessageTime = &at
	}
	remote.msgCalls[id] = 0
	s.RunDue(context.Background())

	batches := store.applied[id]
	require.Len(t, batches, 2)
	second := batches[1]
	// Head was minute 149; overlap of 2 minutes re-reads 147..149 plus the two new ones.
	assert.Len(t, second, 5)
	assert.Equal(t, "new", second[len(second)-1].Message)
	assert.Equal(t, 1, remote.msgCalls[id], "one page is enough")
}

func TestAnIdleAccountCostsOneListPerArchiveState(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	for i := range 2*PageSize + 10 {
		remote.addThread("t", 1, t0.Add(time.Duration(i)*time.Minute))
	}
	s := newTestSyncer(store, remote, Config{MaxThreadsPerRun: 1000})
	store.account.FullWalk = true
	s.RunDue(context.Background())
	require.Equal(t, 2*PageSize+10, store.last().Stats.ThreadsCreated)

	remote.listCalls = 0
	s.RunDue(context.Background())
	assert.Equal(t, 2, remote.listCalls, "first active page unchanged, then the (empty) archive")
	assert.Zero(t, store.last().Stats.ThreadsChecked)
}

func TestABigSyncKeepsGoingPastTheRunCap(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	for i := range 2*PageSize + 20 {
		remote.addThread("t", 1, t0.Add(time.Duration(i)*time.Minute))
	}
	s := newTestSyncer(store, remote, Config{MaxThreadsPerRun: PageSize})
	s.RunDue(context.Background())

	// Rounds of PageSize each; after the first the newest page is unchanged, and a full walk
	// still finds the rest. One run is recorded, with the rounds added up.
	require.Len(t, store.finished, 1)
	assert.Equal(t, models.RemoteSyncStatusOK, store.last().Status)
	assert.Equal(t, 2*PageSize+20, store.last().Stats.ThreadsCreated)
	assert.Zero(t, store.last().Stats.ThreadsDeferred)
	assert.Len(t, store.links, 2*PageSize+20)
}

func TestABigSyncStopsAtAFailingRound(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	var newest uuid.UUID
	for i := range PageSize + 20 {
		newest = remote.addThread("t", 1, t0.Add(time.Duration(i)*time.Minute))
	}
	remote.msgErr[newest] = errors.New("boom")
	s := newTestSyncer(store, remote, Config{MaxThreadsPerRun: PageSize})
	s.RunDue(context.Background())

	require.Len(t, store.finished, 1)
	assert.Equal(t, models.RemoteSyncStatusError, store.last().Status)
	assert.Equal(t, 20, store.last().Stats.ThreadsDeferred, "left for the next run, which walks the whole list")
}

func TestBlankRepliesAreNotCopied(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	id := remote.addThread("t", 4, t0)
	// A reply that failed: blank on the remote, and absent from an account export.
	remote.messages[id][1].Message = "  "
	newTestSyncer(store, remote, Config{}).RunDue(context.Background())

	require.Len(t, store.applied[id], 1)
	got := store.applied[id][0]
	require.Len(t, got, 3)
	for _, m := range got {
		assert.NotEmpty(t, strings.TrimSpace(m.Message))
	}
}

func TestDetachedThreadsAreSeenButNotRead(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	id := remote.addThread("gone", 2, t0)
	store.links[id] = models.RemoteSyncChatLink{RemoteChatID: id, State: models.RemoteSyncLinkDetached}
	s := newTestSyncer(store, remote, Config{})
	s.RunDue(context.Background())

	assert.Zero(t, remote.msgCalls[id], "messages are not fetched")
	require.Len(t, store.applied[id], 1)
	assert.Empty(t, store.applied[id][0])
	assert.NotNil(t, store.links[id].RemoteLastMessageTime, "recorded as seen, so it is not changed next run")
}

func TestARefusedTokenNeedsTheOwner(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.listErr = &Error{Kind: KindForbidden, Status: 403}
	s := newTestSyncer(store, remote, Config{})
	s.RunDue(context.Background())
	res := store.last()
	assert.Equal(t, models.RemoteSyncStatusNeedsAttention, res.Status)
	require.NotNil(t, res.Error)
	assert.Contains(t, *res.Error, "Read threads")
}

func TestOneFailingThreadDoesNotStopTheOthers(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	bad := remote.addThread("bad", 2, t0)
	good := remote.addThread("good", 2, t0.Add(time.Hour))
	remote.msgErr[bad] = &Error{Kind: KindServer, Status: 502}
	deleted := remote.addThread("deleted", 2, t0.Add(2*time.Hour))
	remote.msgErr[deleted] = &Error{Kind: KindNotFound, Status: 404}
	store.applyErr[good] = nil

	s := newTestSyncer(store, remote, Config{})
	s.RunDue(context.Background())
	res := store.last()
	assert.Equal(t, models.RemoteSyncStatusError, res.Status)
	assert.Equal(t, 1, res.Stats.ThreadsFailed)
	assert.Equal(t, 1, res.Stats.ThreadsCreated)
	assert.Contains(t, store.applied, good)
	assert.NotContains(t, store.applied, deleted, "deleted remotely between list and read: skipped, not failed")
}

func TestStartRefusesWhenBusyOrOff(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	s := newTestSyncer(store, remote, Config{})

	store.leased = true
	assert.ErrorIs(t, s.Start(context.Background(), store.account.UserID), ErrBusy)
	store.leased = false

	store.account.Enabled = false
	assert.ErrorIs(t, s.Start(context.Background(), store.account.UserID), ErrDisabled)
	store.account.Enabled = true

	remote.addThread("t", 1, t0)
	require.NoError(t, s.Start(context.Background(), store.account.UserID))
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.finished) == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, models.RemoteSyncStatusOK, store.last().Status)
}

func TestCancelledRunsLeaveNoRecord(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.addThread("t", 1, t0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	remote.listErr = context.Canceled
	newTestSyncer(store, remote, Config{}).RunDue(ctx)
	assert.Empty(t, store.finished)
	assert.True(t, errors.Is(remote.listErr, context.Canceled))
}

func TestARunCancelledAtShutdownReleasesItsLease(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.addThread("t", 1, t0)
	remote.listErr = context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newTestSyncer(store, remote, Config{})
	store.leased = true

	s.runClaimed(ctx, store.account.UserID)
	assert.Empty(t, store.finished, "nothing recorded")
	assert.False(t, store.leased, "the first wake after a restart can run")
}

func TestARunThatPanicsIsRecordedAsFailed(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.listPanic = "boom"
	s := newTestSyncer(store, remote, Config{})
	store.leased = true

	s.runClaimed(context.Background(), store.account.UserID)
	require.Len(t, store.finished, 1)
	assert.Equal(t, models.RemoteSyncStatusError, store.last().Status)
	assert.Contains(t, *store.last().Error, "stopped unexpectedly")
	assert.False(t, store.leased)
}

func TestWakeSyncsOnlyStaleAccountsThatCanRun(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.addThread("t", 1, t0)
	s := newTestSyncer(store, remote, Config{WakeMinAge: time.Minute})
	ctx := context.Background()
	user := store.account.UserID
	finished := func(n int) func() bool {
		return func() bool {
			store.mu.Lock()
			defer store.mu.Unlock()
			return len(store.finished) == n
		}
	}

	// Never run: a wake starts one.
	res, err := s.Wake(ctx, user)
	started := res.Started
	require.NoError(t, err)
	assert.True(t, started)
	require.Eventually(t, finished(1), time.Second, 10*time.Millisecond)

	// Ran moments ago: left alone.
	recent := time.Now().Add(-10 * time.Second)
	store.account.LastRunAt = &recent
	res, err = s.Wake(ctx, user)
	started = res.Started
	require.NoError(t, err)
	assert.False(t, started)

	// Stale again: runs.
	stale := time.Now().Add(-2 * time.Minute)
	store.account.LastRunAt = &stale
	res, err = s.Wake(ctx, user)
	started = res.Started
	require.NoError(t, err)
	assert.True(t, started)
	require.Eventually(t, finished(2), time.Second, 10*time.Millisecond)

	// Off, or waiting on its owner: left alone, and neither is an error.
	store.account.Enabled = false
	res, err = s.Wake(ctx, user)
	started = res.Started
	require.NoError(t, err)
	assert.False(t, started)
	store.account.Enabled, store.account.Status = true, models.RemoteSyncStatusNeedsAttention
	res, err = s.Wake(ctx, user)
	started = res.Started
	require.NoError(t, err)
	assert.False(t, started)

	// Already running: left alone.
	store.account.Status = models.RemoteSyncStatusOK
	store.leased = true
	res, err = s.Wake(ctx, user)
	started = res.Started
	require.NoError(t, err)
	assert.False(t, started)
}

func TestEveryWakeAndRunRematchesPersonas(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.addThread("t", 1, t0)
	s := newTestSyncer(store, remote, Config{})
	ctx := context.Background()

	s.RunDue(ctx)
	assert.Equal(t, 1, store.rematches, "after a run")

	// A fresh account starts no run, but a persona created a moment ago still takes its threads.
	recent := time.Now()
	store.account.LastRunAt = &recent
	store.rematchN = 3
	res, err := s.Wake(ctx, store.account.UserID)
	require.NoError(t, err)
	assert.False(t, res.Started)
	assert.Equal(t, 3, res.Rematched)
	assert.Equal(t, 2, store.rematches)
}

func TestBackgroundRunsAreSpreadOverAFifthOfTheInterval(t *testing.T) {
	lastRun := time.Now()
	seen := map[time.Duration]bool{}
	for range 200 {
		j := backgroundJitter(uuid.New(), lastRun, time.Hour)
		assert.GreaterOrEqual(t, j, time.Duration(0))
		assert.Less(t, j, 12*time.Minute)
		seen[j/time.Minute] = true
	}
	assert.Len(t, seen, 12, "accounts last run at the same moment land all over the window")

	id := uuid.New()
	assert.Equal(t, backgroundJitter(id, lastRun, time.Hour), backgroundJitter(id, lastRun, time.Hour), "stable across ticks")
	assert.Zero(t, backgroundJitter(id, lastRun, 0))
}

func TestRunDueWaitsOutTheAccountsJitter(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.addThread("t", 1, t0)
	s := newTestSyncer(store, remote, Config{Interval: time.Hour})
	ctx := context.Background()

	// Just past the interval, yet before this account's own jitter: not yet.
	lastRun := time.Now().Add(-time.Hour - time.Second)
	for backgroundJitter(store.account.UserID, lastRun, time.Hour) < time.Minute {
		lastRun = lastRun.Add(-time.Microsecond)
	}
	store.account.LastRunAt = &lastRun
	s.RunDue(ctx)
	assert.Empty(t, store.finished)

	// Past the whole window: due whatever the jitter.
	stale := time.Now().Add(-time.Hour * 6 / 5)
	store.account.LastRunAt = &stale
	s.RunDue(ctx)
	require.Len(t, store.finished, 1)
}

func TestTheRemoteSummaryComesWithAThreadsMessages(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	id := remote.addThread("t", 4, t0)
	start := t0.Add(2 * time.Minute)
	remote.summaries = map[uuid.UUID]*models.ChatSummary{id: {ChatID: id, Summary: "So far.", WindowStart: &start}}
	s := newTestSyncer(store, remote, Config{})

	s.RunDue(context.Background())
	require.Len(t, store.summaries, 1)
	require.NotNil(t, store.summaries[0])
	assert.Equal(t, "So far.", store.summaries[0].Summary)

	// An unchanged thread is not read again, summary included.
	hits := remote.summaryHits
	s.RunDue(context.Background())
	assert.Equal(t, hits, remote.summaryHits)
}

func TestNoRemoteSummaryIsNotAFailure(t *testing.T) {
	for name, setup := range map[string]func(r *fakeRemote, id uuid.UUID){
		"none yet":         func(r *fakeRemote, id uuid.UUID) { r.summaries = map[uuid.UUID]*models.ChatSummary{id: {ChatID: id}} },
		"old server (404)": func(*fakeRemote, uuid.UUID) {},
		"server error":     func(r *fakeRemote, _ uuid.UUID) { r.summaryErr = &Error{Kind: KindServer, Status: 502} },
	} {
		t.Run(name, func(t *testing.T) {
			remote, store := newFakeRemote(), newFakeStore()
			setup(remote, remote.addThread("t", 2, t0))
			newTestSyncer(store, remote, Config{}).RunDue(context.Background())
			assert.Equal(t, models.RemoteSyncStatusOK, store.last().Status)
			require.Len(t, store.summaries, 1)
			assert.Nil(t, store.summaries[0], "the copy is summarized here instead")
		})
	}
}

func TestARefusedTokenOnTheSummaryStopsTheRun(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	remote.addThread("t", 2, t0)
	remote.summaryErr = &Error{Kind: KindUnauthorized, Status: 401}
	newTestSyncer(store, remote, Config{}).RunDue(context.Background())
	assert.Equal(t, models.RemoteSyncStatusNeedsAttention, store.last().Status)
}
