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
	q := &TurnQueue[int]{Depth: 2, Idle: 50 * time.Millisecond, Run: func(context.Context, int) {
		n := running.Add(1)
		for {
			m := maxRunning.Load()
			if n <= m || maxRunning.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		ran.Add(1)
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
