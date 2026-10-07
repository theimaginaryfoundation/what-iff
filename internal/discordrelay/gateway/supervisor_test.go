package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"go.uber.org/zap"
)

type fakeSource struct {
	mu   sync.Mutex
	bots []Bot
	err  error
}

func (f *fakeSource) ActiveBots(context.Context) ([]Bot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Bot(nil), f.bots...), f.err
}

func (f *fakeSource) set(bots ...Bot) {
	f.mu.Lock()
	f.bots = bots
	f.mu.Unlock()
}

type fakeConn struct {
	mu     sync.Mutex
	closed bool
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *fakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type fakeDialer struct {
	mu      sync.Mutex
	dials   map[uuid.UUID]int
	conns   map[uuid.UUID]*fakeConn
	deliver map[uuid.UUID]func(discordrelay.InboundMessage)
	fail    map[uuid.UUID]bool
}

func newFakeDialer() *fakeDialer {
	return &fakeDialer{
		dials:   map[uuid.UUID]int{},
		conns:   map[uuid.UUID]*fakeConn{},
		deliver: map[uuid.UUID]func(discordrelay.InboundMessage){},
		fail:    map[uuid.UUID]bool{},
	}
}

func (d *fakeDialer) dial(_ context.Context, bot Bot, on func(discordrelay.InboundMessage)) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials[bot.ID]++
	if d.fail[bot.ID] {
		return nil, errors.New("bad token")
	}
	c := &fakeConn{}
	d.conns[bot.ID] = c
	d.deliver[bot.ID] = on
	return c, nil
}

func (d *fakeDialer) conn(id uuid.UUID) *fakeConn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conns[id]
}

func (d *fakeDialer) dialCount(id uuid.UUID) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials[id]
}

type recordingSink struct {
	mu   sync.Mutex
	seen []string
}

func (r *recordingSink) HandleInbound(_ context.Context, bot Bot, msg discordrelay.InboundMessage) {
	r.mu.Lock()
	r.seen = append(r.seen, bot.BotUserID+":"+msg.ID)
	r.mu.Unlock()
}

func newSupervisor(src BotSource, d *fakeDialer, sink Sink, locker Locker) *Supervisor {
	return &Supervisor{
		Locker:         locker,
		Source:         src,
		Sink:           sink,
		Dial:           d.dial,
		Logger:         zap.NewNop(),
		ReconcileEvery: 10 * time.Millisecond,
		RetryEvery:     10 * time.Millisecond,
	}
}

func TestReconcileOpensNewBotsAndClosesRemovedOnes(t *testing.T) {
	a, b := Bot{ID: uuid.New(), Token: "ta"}, Bot{ID: uuid.New(), Token: "tb"}
	src := &fakeSource{bots: []Bot{a, b}}
	d := newFakeDialer()
	s := newSupervisor(src, d, &recordingSink{}, SingleInstanceLocker{})

	s.Reconcile(context.Background())
	assert.ElementsMatch(t, []uuid.UUID{a.ID, b.ID}, s.Connected())

	src.set(a)
	s.Reconcile(context.Background())
	assert.ElementsMatch(t, []uuid.UUID{a.ID}, s.Connected())
	assert.True(t, d.conn(b.ID).isClosed())
	assert.Equal(t, 1, d.dialCount(a.ID), "an unchanged bot is not reconnected")
}

func TestReconcileReconnectsWhenTheTokenOrIntentChanges(t *testing.T) {
	a := Bot{ID: uuid.New(), Token: "old"}
	src := &fakeSource{bots: []Bot{a}}
	d := newFakeDialer()
	s := newSupervisor(src, d, &recordingSink{}, SingleInstanceLocker{})
	s.Reconcile(context.Background())
	first := d.conn(a.ID)

	a.Token = "new"
	src.set(a)
	s.Reconcile(context.Background())
	assert.True(t, first.isClosed())
	assert.Equal(t, 2, d.dialCount(a.ID))

	a.MessageContent = true
	src.set(a)
	s.Reconcile(context.Background())
	assert.Equal(t, 3, d.dialCount(a.ID))
}

func TestReconcileRetriesAFailedConnectNextTime(t *testing.T) {
	a := Bot{ID: uuid.New(), Token: "t"}
	d := newFakeDialer()
	d.fail[a.ID] = true
	s := newSupervisor(&fakeSource{bots: []Bot{a}}, d, &recordingSink{}, SingleInstanceLocker{})

	s.Reconcile(context.Background())
	assert.Empty(t, s.Connected())

	d.mu.Lock()
	d.fail[a.ID] = false
	d.mu.Unlock()
	s.Reconcile(context.Background())
	assert.ElementsMatch(t, []uuid.UUID{a.ID}, s.Connected())
}

func TestReconcileKeepsConnectionsWhenListingFails(t *testing.T) {
	a := Bot{ID: uuid.New(), Token: "t"}
	src := &fakeSource{bots: []Bot{a}}
	d := newFakeDialer()
	s := newSupervisor(src, d, &recordingSink{}, SingleInstanceLocker{})
	s.Reconcile(context.Background())

	src.mu.Lock()
	src.err = errors.New("db down")
	src.mu.Unlock()
	s.Reconcile(context.Background())
	assert.ElementsMatch(t, []uuid.UUID{a.ID}, s.Connected())
}

func TestMessagesReachTheSinkWithTheirBot(t *testing.T) {
	a := Bot{ID: uuid.New(), Token: "t", BotUserID: "111"}
	d := newFakeDialer()
	sink := &recordingSink{}
	s := newSupervisor(&fakeSource{bots: []Bot{a}}, d, sink, SingleInstanceLocker{})
	s.Reconcile(context.Background())

	d.deliver[a.ID](discordrelay.InboundMessage{ID: "m1"})
	assert.Equal(t, []string{"111:m1"}, sink.seen)
}

// flakyLock reports healthy until told otherwise.
type flakyLock struct {
	mu       sync.Mutex
	healthy  bool
	released bool
}

func (l *flakyLock) IsHealthy(context.Context) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.healthy, nil
}

func (l *flakyLock) Release(context.Context) error {
	l.mu.Lock()
	l.released = true
	l.mu.Unlock()
	return nil
}

type oneShotLocker struct {
	mu    sync.Mutex
	lock  *flakyLock
	taken bool
}

func (o *oneShotLocker) TryAcquire(context.Context) (Lock, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.taken {
		return nil, false, nil
	}
	o.taken = true
	return o.lock, true, nil
}

func TestRunClosesEverythingAndReleasesWhenTheLockIsLost(t *testing.T) {
	a := Bot{ID: uuid.New(), Token: "t"}
	d := newFakeDialer()
	lock := &flakyLock{healthy: true}
	s := newSupervisor(&fakeSource{bots: []Bot{a}}, d, &recordingSink{}, &oneShotLocker{lock: lock})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	require.Eventually(t, func() bool { return len(s.Connected()) == 1 }, time.Second, 5*time.Millisecond)

	lock.mu.Lock()
	lock.healthy = false
	lock.mu.Unlock()

	require.Eventually(t, func() bool {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		return lock.released
	}, time.Second, 5*time.Millisecond)
	assert.Empty(t, s.Connected())
	assert.True(t, d.conn(a.ID).isClosed())

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunStopsOnCancelAndReleases(t *testing.T) {
	a := Bot{ID: uuid.New(), Token: "t"}
	d := newFakeDialer()
	lock := &flakyLock{healthy: true}
	s := newSupervisor(&fakeSource{bots: []Bot{a}}, d, &recordingSink{}, &oneShotLocker{lock: lock})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { return len(s.Connected()) == 1 }, time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
	assert.True(t, lock.released)
	assert.True(t, d.conn(a.ID).isClosed())
}

func TestToInboundMapsAMessage(t *testing.T) {
	m := &discordgo.Message{
		ID: "m", GuildID: "g", ChannelID: "c", Content: "<@111> hi",
		Author:   &discordgo.User{ID: "222", Username: "alice_", GlobalName: "Alice"},
		Member:   &discordgo.Member{Nick: "Al"},
		Mentions: []*discordgo.User{{ID: "111"}},
		ReferencedMessage: &discordgo.Message{
			ID: "r", Content: "earlier", Author: &discordgo.User{ID: "111", Username: "vix"},
		},
	}

	in := ToInbound(nil, m)

	assert.Equal(t, discordrelay.InboundMessage{
		ID: "m", GuildID: "g", ChannelID: "c", Content: "<@111> hi",
		AuthorID: "222", AuthorName: "Al",
		MentionUserIDs:      []string{"111"},
		ReferencedMessageID: "r", ReferencedAuthorID: "111", ReferencedAuthorName: "vix", ReferencedContent: "earlier",
	}, in)
}

func TestToInboundCarriesAttachments(t *testing.T) {
	m := &discordgo.Message{
		ID: "m", ChannelID: "c", Content: "<@111> look",
		Attachments: []*discordgo.MessageAttachment{
			{Filename: "cat.png", ContentType: "image/png", URL: "https://cdn.discordapp.com/a/cat.png", Size: 1234},
			{Filename: "nourl.png"}, // nothing to fetch
			nil,
		},
	}

	in := ToInbound(nil, m)

	assert.Equal(t, []discordrelay.Attachment{
		{Filename: "cat.png", ContentType: "image/png", URL: "https://cdn.discordapp.com/a/cat.png", Size: 1234},
	}, in.Attachments)
}
