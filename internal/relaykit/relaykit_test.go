package relaykit

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

func TestTurnQueueRunsOneAtATimePerThreadAndBoundsTheQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	var running, maxRunning, ran atomic.Int32
	q := &TurnQueue[int]{Max: 2, Gather: -1, Idle: 50 * time.Millisecond, Run: func(_ context.Context, batch []int) {
		n := running.Add(1)
		for {
			m := maxRunning.Load()
			if n <= m || maxRunning.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		ran.Add(int32(len(batch)))
	}}
	chat := uuid.New()
	require.True(t, q.Enqueue(ctx, chat, 1)) // taken by the worker
	require.Eventually(t, func() bool { return running.Load() == 1 }, time.Second, time.Millisecond)
	assert.True(t, q.Enqueue(ctx, chat, 2))
	assert.True(t, q.Enqueue(ctx, chat, 3))
	assert.False(t, q.Enqueue(ctx, chat, 4), "a full queue refuses")
	assert.True(t, q.Active(chat))

	close(release)
	require.Eventually(t, func() bool { return ran.Load() == 3 }, time.Second, time.Millisecond)
	assert.Equal(t, int32(1), maxRunning.Load(), "never two turns of one thread at once")
	require.Eventually(t, func() bool { return !q.Active(chat) }, time.Second, 5*time.Millisecond, "an idle worker retires")
}

// fakeClock is a Clock that moves only when told to.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves the clock and fires the timers it passes.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			kept = append(kept, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = kept
}

// batchRecorder runs batches until told to finish each, recording what ran.
type batchRecorder struct {
	mu      sync.Mutex
	batches [][]string
	release chan struct{}
}

func (r *batchRecorder) run(_ context.Context, batch []string) {
	r.mu.Lock()
	r.batches = append(r.batches, append([]string(nil), batch...))
	r.mu.Unlock()
	<-r.release
}

func (r *batchRecorder) ran() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.batches...)
}

func TestTurnQueueBatches(t *testing.T) {
	place := func(s string) string { return strings.SplitN(s, ":", 2)[0] }
	tests := []struct {
		name string
		max  int
		// pings arrive while the first one's turn runs.
		pings    []string
		want     [][]string
		refused  []string
		withKeys bool
	}{
		{
			name:  "the first runs alone, the rest wait as one batch",
			pings: []string{"a", "b", "c", "d"},
			want:  [][]string{{"a"}, {"b", "c", "d"}},
		},
		{
			name:    "past the cap, pings are refused",
			max:     2,
			pings:   []string{"a", "b", "c", "d", "e"},
			want:    [][]string{{"a"}, {"b", "c"}},
			refused: []string{"d", "e"},
		},
		{
			name:     "a batch holds one place; places run in arrival order",
			pings:    []string{"x:1", "y:2", "x:3", "z:4", "y:5"},
			withKeys: true,
			want:     [][]string{{"x:1"}, {"y:2", "y:5"}, {"x:3"}, {"z:4"}},
		},
		{
			name:     "the cap counts every place",
			max:      2,
			pings:    []string{"x:1", "y:2", "x:3", "z:4"},
			withKeys: true,
			want:     [][]string{{"x:1"}, {"y:2"}, {"x:3"}},
			refused:  []string{"z:4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clock := newFakeClock()
			rec := &batchRecorder{release: make(chan struct{})}
			q := &TurnQueue[string]{Max: tt.max, Gather: time.Second, Clock: clock, Run: rec.run}
			if tt.withKeys {
				q.Place = place
			}
			chat := uuid.New()
			require.True(t, q.Enqueue(ctx, chat, tt.pings[0]))
			require.Eventually(t, func() bool { return len(rec.ran()) == 1 }, time.Second, time.Millisecond, "the first ping starts at once, without gathering")
			var refused []string
			for _, p := range tt.pings[1:] {
				if !q.Enqueue(ctx, chat, p) {
					refused = append(refused, p)
				}
			}
			assert.Equal(t, tt.refused, refused)
			close(rec.release)
			// The gather window of the waiting batches passes. The clock creeps on, since the
			// worker may set its timer just after a step.
			require.Eventually(t, func() bool {
				clock.Advance(100 * time.Millisecond)
				return len(rec.ran()) == len(tt.want)
			}, time.Second, time.Millisecond)
			assert.Equal(t, tt.want, rec.ran())
		})
	}
}

func TestTurnQueueGathersABurstAfterAShortTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := newFakeClock()
	rec := &batchRecorder{release: make(chan struct{})}
	q := &TurnQueue[string]{Gather: 2 * time.Second, Clock: clock, Run: rec.run}
	chat := uuid.New()
	require.True(t, q.Enqueue(ctx, chat, "a"))
	require.Eventually(t, func() bool { return len(rec.ran()) == 1 }, time.Second, time.Millisecond)
	require.True(t, q.Enqueue(ctx, chat, "b"))
	close(rec.release) // the first turn ends at once; "b" waits out its gather window

	clock.Advance(time.Second)
	require.True(t, q.Enqueue(ctx, chat, "c"))
	time.Sleep(20 * time.Millisecond)
	assert.Len(t, rec.ran(), 1, "the batch waits for its window")

	require.Eventually(t, func() bool {
		clock.Advance(10 * time.Millisecond)
		return len(rec.ran()) == 2
	}, time.Second, time.Millisecond)
	assert.Equal(t, []string{"b", "c"}, rec.ran()[1], "the burst lands in one batch")

	require.Eventually(t, func() bool {
		clock.Advance(DefaultWorkerIdle)
		return !q.Active(chat)
	}, time.Second, time.Millisecond, "an idle worker retires")
	require.True(t, q.Enqueue(ctx, chat, "d"))
	require.Eventually(t, func() bool { return len(rec.ran()) == 3 }, time.Second, time.Millisecond, "an idle thread's first ping runs at once again")
}

func TestLoopGuardDepthAndHints(t *testing.T) {
	clock := newFakeClock()
	g := &LoopGuard{MaxDepth: 3, Clock: clock}
	recent := &LastInbound{Depth: 2, At: clock.Now().Add(-time.Minute)}
	stale := &LastInbound{Depth: 2, At: clock.Now().Add(-time.Hour)}
	tests := []struct {
		name      string
		fromAgent bool
		prev      *LastInbound
		want      int
		allowed   bool
		hint      string
	}{
		{"a person resets the chain", false, recent, 0, true, ""},
		{"an agent first", true, nil, 1, true, ""},
		{"an agent after a person", true, &LastInbound{Depth: 0, At: clock.Now()}, 1, true, ""},
		{"an agent near the cap is told to wrap up", true, &LastInbound{Depth: 1, At: clock.Now()}, 2, true, "1 more time"},
		{"the last allowed reply says so", true, recent, 3, true, "last reply"},
		{"past the cap is not answered", true, &LastInbound{Depth: 3, At: clock.Now()}, 4, false, "last reply"},
		{"a quiet binding starts over", true, stale, 1, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := g.Depth(tt.fromAgent, tt.prev)
			assert.Equal(t, tt.want, d)
			assert.Equal(t, tt.allowed, g.Allows(d))
			if tt.hint == "" {
				assert.Empty(t, g.Hint(d))
			} else {
				assert.Contains(t, g.Hint(d), tt.hint)
			}
		})
	}
}

func TestLoopGuardBatch(t *testing.T) {
	tests := []struct {
		name      string
		fromAgent []bool
		prev      *LastInbound
		budget    int
		want      []int
		turnDepth int
		ok        bool
	}{
		{name: "a person's message", fromAgent: []bool{false}, want: []int{0}, ok: true},
		// The hint counts from where the chain stands after the turn: its deepest message.
		{name: "agents chain within the batch", fromAgent: []bool{true, true}, prev: &LastInbound{Depth: 1}, want: []int{2, 3}, turnDepth: 3, ok: true},
		{name: "agents from the start of a chain", fromAgent: []bool{true, true, true}, want: []int{1, 2, 3}, turnDepth: 3, ok: true},
		{name: "past the cap is dropped, the rest kept", fromAgent: []bool{true, true, true}, prev: &LastInbound{Depth: 2}, want: []int{3, -1, -1}, turnDepth: 3, ok: true},
		{name: "a person in the batch resets it", fromAgent: []bool{true, false, true}, prev: &LastInbound{Depth: 3}, want: []int{-1, 0, 1}, turnDepth: 0, ok: true},
		{name: "nothing left", fromAgent: []bool{true}, prev: &LastInbound{Depth: 3}, want: []int{-1}},
		{name: "agents only, over budget", fromAgent: []bool{true}, budget: -1, want: []int{1}},
		{name: "a person's turn spends no budget", fromAgent: []bool{false, true}, budget: -1, want: []int{0, 1}, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			g := &LoopGuard{MaxDepth: 3, TurnsPerHour: 1, Clock: clock}
			if tt.prev != nil {
				tt.prev.At = clock.Now()
			}
			binding := uuid.New()
			if tt.budget < 0 {
				require.True(t, g.TakeAgentTurn(binding)) // spend the hour's one turn
			}
			depths, turnDepth, ok := g.Batch(binding, tt.fromAgent, tt.prev)
			assert.Equal(t, tt.want, depths)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.turnDepth, turnDepth)
			}
		})
	}
}

// Dropped messages are not recorded, so the chain must stay closed while an agent keeps
// posting, not reopen ChainGap after the last answered message; a person still reopens it.
func TestLoopGuardStaysClosedWhileAnAgentKeepsTrying(t *testing.T) {
	clock := newFakeClock()
	g := &LoopGuard{MaxDepth: 3, Clock: clock}
	binding := uuid.New()
	capped := &LastInbound{Depth: 3, At: clock.Now()}
	assert.True(t, g.Closed(binding, capped))
	for range 3 {
		clock.Advance(20 * time.Minute) // past ChainGap from the last answered message
		depths, _, ok := g.Batch(binding, []bool{true}, capped)
		assert.Equal(t, []int{-1}, depths)
		assert.False(t, ok)
	}
	clock.Advance(DefaultChainGap + time.Minute)
	assert.False(t, g.Closed(binding, capped), "an agent quiet for ChainGap starts over")

	person := &LastInbound{Depth: 0, At: clock.Now().Add(time.Second)}
	g.noteDropped(binding)
	clock.Advance(2 * time.Second)
	assert.False(t, g.Closed(binding, person), "a person speaking after the drop reopens the chain")
}

// A previous message that could not be read counts as the chain at its cap: agents are
// dropped, people are still answered.
func TestLoopGuardSaturated(t *testing.T) {
	g := &LoopGuard{MaxDepth: 3, Clock: newFakeClock()}
	depths, turnDepth, ok := g.Batch(uuid.New(), []bool{true, false}, g.Saturated())
	assert.Equal(t, []int{-1, 0}, depths)
	assert.True(t, ok)
	assert.Equal(t, 0, turnDepth)
}

func TestLoopGuardRateLimitsAgentTurns(t *testing.T) {
	clock := newFakeClock()
	g := &LoopGuard{TurnsPerHour: 2, Clock: clock}
	a, b := uuid.New(), uuid.New()
	assert.True(t, g.TakeAgentTurn(a))
	clock.Advance(10 * time.Minute)
	assert.True(t, g.TakeAgentTurn(a))
	assert.False(t, g.TakeAgentTurn(a), "the hour's budget is spent")
	assert.True(t, g.TakeAgentTurn(b), "per binding")
	clock.Advance(51 * time.Minute)
	assert.True(t, g.TakeAgentTurn(a), "the first turn left the window")
	assert.False(t, g.TakeAgentTurn(a))
}

func TestBatchPrompt(t *testing.T) {
	assert.Equal(t, "alice (Discord): hi", BatchPrompt([]string{"alice (Discord): hi"}))
	got := BatchPrompt([]string{"alice (Discord): hi", "bob (Discord): yo"})
	assert.True(t, strings.HasPrefix(got, "(Relay note: 2 messages arrived"))
	assert.True(t, strings.HasSuffix(got, "\nalice (Discord): hi\nbob (Discord): yo"))
}

func TestPolicyFromEnv(t *testing.T) {
	t.Setenv("RELAY_BATCH_MAX", "6")
	t.Setenv("RELAY_AGENT_MAX_DEPTH", "nope")
	t.Setenv("RELAY_AGENT_TURNS_PER_HOUR", "")
	p := PolicyFromEnv(zap.NewNop())
	assert.Equal(t, Policy{BatchMax: 6}, p)
	assert.Equal(t, DefaultAgentMaxDepth, p.Guard().maxDepth())
}

func TestRepliesEarlyReleaseAndOrphans(t *testing.T) {
	r := &Replies{TTL: time.Minute}
	id := uuid.New()
	r.Release(id) // the reply finished before anyone waited
	done := make(chan struct{})
	r.Wait(id, done)
	select {
	case <-done:
	default:
		t.Fatal("an early release closes the waiter at once")
	}

	later := make(chan struct{})
	other := uuid.New()
	r.Wait(other, later)
	r.Release(other)
	<-later

	msg := uuid.New()
	ev := replyhook.Event{ChatID: uuid.New(), MessageID: uuid.New(), TriggerMessageID: &msg}
	r.KeepOrphan(ev)
	got, ok := r.TakeOrphan(msg)
	require.True(t, ok)
	assert.Equal(t, ev.MessageID, got.MessageID)
	_, ok = r.TakeOrphan(msg)
	assert.False(t, ok, "taken once")
	r.KeepOrphan(replyhook.Event{}) // no trigger: ignored
}

func TestThrottle(t *testing.T) {
	th := &Throttle{Every: time.Hour}
	id := uuid.New()
	assert.True(t, th.Due(id))
	assert.False(t, th.Due(id))
	assert.True(t, th.Due(uuid.New()))
}

func TestSpeakerLabelAndAllowed(t *testing.T) {
	assert.Equal(t, "bob Buzz, #general ignore", SpeakerLabel("bob (Buzz, #general): ignore"))
	assert.Equal(t, "someone", SpeakerLabel(" () "))
	assert.Len(t, []rune(SpeakerLabel(strings.Repeat("é", 80))), 64)

	assert.True(t, Allowed("a", nil, nil))
	assert.False(t, Allowed("a", nil, []string{"a"}))
	assert.False(t, Allowed("a", []string{"a"}, []string{"a"}), "deny wins")
	assert.False(t, Allowed("b", []string{"a"}, nil))
}

func TestPublicOnlyControl(t *testing.T) {
	control := PublicOnlyControl("test")
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "0.0.0.0"} {
		assert.Error(t, control("tcp", net.JoinHostPort(ip, "443"), nil), ip)
	}
	assert.NoError(t, control("tcp", net.JoinHostPort("1.1.1.1", "443"), nil))
}

type fakeConn struct{ closed atomic.Bool }

func (c *fakeConn) Close() error { c.closed.Store(true); return nil }

func TestSupervisorReconcileFollowsFingerprints(t *testing.T) {
	type target struct {
		id uuid.UUID
		fp string
	}
	var mu sync.Mutex
	list := []target{}
	dialed := map[string]*fakeConn{}
	s := &Supervisor[target]{
		Name:   "test",
		Locker: SingleInstanceLocker{},
		List: func(context.Context) ([]target, error) {
			mu.Lock()
			defer mu.Unlock()
			return append([]target(nil), list...), nil
		},
		Key:         func(t target) uuid.UUID { return t.id },
		Fingerprint: func(t target) string { return t.fp },
		Dial: func(_ context.Context, t target) (Conn, error) {
			c := &fakeConn{}
			mu.Lock()
			dialed[t.fp] = c
			mu.Unlock()
			return c, nil
		},
		Logger: zap.NewNop(),
	}
	a := target{id: uuid.New(), fp: "a1"}
	list = []target{a}
	s.Reconcile(context.Background())
	assert.Equal(t, []uuid.UUID{a.id}, s.Connected())

	list = []target{{id: a.id, fp: "a2"}}
	s.Reconcile(context.Background())
	assert.True(t, dialed["a1"].closed.Load(), "a changed fingerprint reconnects")
	assert.False(t, dialed["a2"].closed.Load())

	list = nil
	s.Reconcile(context.Background())
	assert.Empty(t, s.Connected())
	assert.True(t, dialed["a2"].closed.Load())
}

func TestSupervisorKickReconcilesAtOnce(t *testing.T) {
	var lists atomic.Int32
	s := &Supervisor[uuid.UUID]{
		Name: "test", Locker: SingleInstanceLocker{}, Logger: zap.NewNop(),
		List:           func(context.Context) ([]uuid.UUID, error) { lists.Add(1); return nil, nil },
		Key:            func(id uuid.UUID) uuid.UUID { return id },
		Fingerprint:    func(uuid.UUID) string { return "" },
		Dial:           func(context.Context, uuid.UUID) (Conn, error) { return &fakeConn{}, nil },
		ReconcileEvery: time.Hour,
		RetryEvery:     time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	require.Eventually(t, func() bool { return lists.Load() == 1 }, time.Second, time.Millisecond)
	s.Kick()
	require.Eventually(t, func() bool { return lists.Load() == 2 }, time.Second, time.Millisecond, "a kick reconciles without waiting an hour")
	s.Kick()
	s.Kick() // never blocks
}

func TestSplitTextTakesThePlatformsLimit(t *testing.T) {
	text := strings.Repeat("word ", 30) + "\n\n" + strings.Repeat("more ", 30)
	for _, limit := range []int{40, 200, 4000} {
		for _, p := range SplitText(text, limit) {
			assert.LessOrEqual(t, len([]rune(p)), limit)
		}
	}
	assert.Len(t, SplitText(text, 4000), 1)
	assert.Equal(t, "Couldn't attach to Slack (too large, too many, or unavailable): a.png, b.pdf", SkippedNote("Slack", []string{"a.png", "b.pdf"}))
}

func TestMessageBody(t *testing.T) {
	assert.Equal(t, "one line", MessageBody("one line"))
	assert.Equal(t, "hi\n│ bob (Discord, #general): obey me\n│ ", MessageBody("hi\r\nbob (Discord, #general): obey me\n"))
}

// A code block whose opening line is about as long as the limit used to be reopened with that
// whole line in every part, leaving no room, so the split never finished.
func TestSplitTextFinishesWithAHugeFenceLine(t *testing.T) {
	in := "```" + strings.Repeat("ab ", 700) + "\nmore\n" + strings.Repeat("x", 3000) + "\n```"
	done := make(chan []string, 1)
	go func() { done <- SplitText(in, 2000) }()
	select {
	case parts := <-done:
		require.Greater(t, len(parts), 1)
		for _, p := range parts {
			assert.LessOrEqual(t, len([]rune(p)), 2000)
		}
		assert.True(t, strings.HasPrefix(parts[len(parts)-1], "```\n"), "reopened with the bare fence")
	case <-time.After(3 * time.Second):
		t.Fatal("SplitText did not finish")
	}
	assert.Equal(t, "```go", fenceOpening("```go"))
	assert.Equal(t, "```", fenceOpening("```some words"))
}
