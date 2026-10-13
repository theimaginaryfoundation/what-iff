package remotesync

import (
	"context"
	"errors"
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
	msgErr   map[uuid.UUID]error

	listCalls int
	msgCalls  map[uuid.UUID]int
}

func newFakeRemote() *fakeRemote {
	return &fakeRemote{messages: map[uuid.UUID][]models.WebhookMessage{}, msgErr: map[uuid.UUID]error{}, msgCalls: map[uuid.UUID]int{}}
}

func (r *fakeRemote) Host() string { return "remote.test" }

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
	mu       sync.Mutex
	account  models.RemoteSyncAccount
	links    map[uuid.UUID]models.RemoteSyncChatLink
	applied  map[uuid.UUID][][]models.RemoteSyncMessage
	applyErr map[uuid.UUID]error
	leased   bool
	finished []models.RemoteSyncRunResult
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

func (s *fakeStore) ListDueRemoteSyncs(context.Context, time.Time) ([]uuid.UUID, error) {
	return []uuid.UUID{s.account.UserID}, nil
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

func TestTheRunCapDefersAndTheNextRunWalksOn(t *testing.T) {
	remote, store := newFakeRemote(), newFakeStore()
	for i := range PageSize + 20 {
		remote.addThread("t", 1, t0.Add(time.Duration(i)*time.Minute))
	}
	s := newTestSyncer(store, remote, Config{MaxThreadsPerRun: PageSize})
	s.RunDue(context.Background())
	assert.Equal(t, 20, store.last().Stats.ThreadsDeferred)

	// The first page is now all unchanged; a full walk still finds the rest.
	s.RunDue(context.Background())
	assert.Equal(t, 20, store.last().Stats.ThreadsCreated)
	assert.Zero(t, store.last().Stats.ThreadsDeferred)
	assert.Len(t, store.links, PageSize+20)
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
