// Package relaykit is the machinery the chat relays (Discord, Buzz) share: one turn at a
// time per relay thread, batching what arrives meanwhile, the agent-to-agent loop guard, the
// bookkeeping that matches a finished reply to the tag that asked for it, a leader-locked supervisor for long-lived connections, the
// public-address guard for outbound connections, and the speaker-label and allow-list rules.
// See docs/adr/0x025-discord-relay.md, docs/adr/0x028-buzz-relay.md and
// docs/adr/0x031-relay-turn-batching-and-loop-guard.md.
//
// Nothing here knows about a particular platform; each relay keeps its own storage, wire
// format and trigger rules.
package relaykit

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultWorkerIdle is how long a thread's worker waits for more work before it exits.
const DefaultWorkerIdle = time.Minute

// DefaultBatchMax is how many messages may wait for a thread's next turn (across its
// batches) before more are refused. A spammed persona answers them in one reply each batch,
// so this bounds what a burst can make it read, not how many replies it posts.
const DefaultBatchMax = 8

// DefaultGather is how long a waiting batch is held back after its first message, so a burst
// that arrives just as the running turn ends still lands in one batch.
const DefaultGather = 2 * time.Second

// Clock is the time source of the queue and the loop guard; tests use a fake one.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// RealClock is the wall clock.
var RealClock Clock = realClock{}

// TurnQueue runs a relay thread's turns one at a time, batching what arrives meanwhile.
//
// The first message of an idle thread starts a turn at once, on its own. Messages that arrive
// while that turn runs wait, grouped by place (Place: a Discord thread, a Buzz thread), and
// each group runs as one turn when its time comes: the persona reads them all and writes one
// reply, so a burst of tags costs one turn, not one each. At most Max messages wait per thread;
// Enqueue refuses the rest. Each thread gets a worker on first use, which exits once idle, so
// idle threads hold nothing.
type TurnQueue[W any] struct {
	// Max bounds the messages waiting per thread, across its batches (DefaultBatchMax when zero).
	Max int
	// Place is where a message's reply goes; a batch holds messages of one place only, since
	// its one reply is posted to one place. Nil puts every message of a thread in one place.
	// It runs under the queue's lock, so it must be cheap and pure (no I/O).
	Place func(W) string
	// Gather holds a waiting batch back until its first message is this old (DefaultGather
	// when zero, none when negative). The first message of an idle thread never waits.
	Gather time.Duration
	// Idle is how long a worker waits before exiting (DefaultWorkerIdle when zero).
	Idle time.Duration
	// Run handles one batch, oldest message first. It runs on the thread's worker, so turns
	// of one thread never overlap.
	Run func(ctx context.Context, batch []W)
	// Clock is the time source (RealClock when nil).
	Clock Clock

	mu      sync.Mutex
	threads map[uuid.UUID]*threadTurns[W]
}

// threadTurns is one thread's state: whether a turn is running and the batches waiting.
type threadTurns[W any] struct {
	running bool
	batches []*waitingBatch[W]
	waiting int
	// wake tells the worker that a batch was added.
	wake chan struct{}
}

type waitingBatch[W any] struct {
	place string
	items []W
	since time.Time
	// now runs without gathering: the thread was idle when its first message came.
	now bool
}

func (q *TurnQueue[W]) clock() Clock {
	if q.Clock == nil {
		return RealClock
	}
	return q.Clock
}

func (q *TurnQueue[W]) max() int {
	if q.Max <= 0 {
		return DefaultBatchMax
	}
	return q.Max
}

func (q *TurnQueue[W]) gather() time.Duration {
	switch {
	case q.Gather < 0:
		return 0
	case q.Gather == 0:
		return DefaultGather
	}
	return q.Gather
}

// Enqueue adds w to the thread's turns, starting its worker on first use, and reports false
// when Max messages are already waiting. It joins the waiting batch of its place, or opens one.
// Everything happens under the lock the worker takes before it retires, so a message can
// never land in a thread whose worker has just exited.
func (q *TurnQueue[W]) Enqueue(ctx context.Context, chatID uuid.UUID, w W) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.threads == nil {
		q.threads = map[uuid.UUID]*threadTurns[W]{}
	}
	t, ok := q.threads[chatID]
	if !ok {
		t = &threadTurns[W]{wake: make(chan struct{}, 1)}
		q.threads[chatID] = t
		go q.work(ctx, chatID, t)
	}
	idle := !t.running && t.waiting == 0
	if !idle && t.waiting >= q.max() {
		return false
	}
	place := ""
	if q.Place != nil {
		place = q.Place(w)
	}
	var b *waitingBatch[W]
	for _, cur := range t.batches {
		// The batch that runs at once is the idle thread's first message alone.
		if cur.place == place && !cur.now {
			b = cur
			break
		}
	}
	if b == nil {
		b = &waitingBatch[W]{place: place, since: q.clock().Now(), now: idle}
		t.batches = append(t.batches, b)
	}
	b.items = append(b.items, w)
	t.waiting++
	select {
	case t.wake <- struct{}{}:
	default:
	}
	return true
}

// Active reports whether the thread has a worker (a turn running or recently run).
func (q *TurnQueue[W]) Active(chatID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.threads[chatID]
	return ok
}

// MarkActive registers an idle thread without a worker, as if a turn were running there. It
// exists for tests of what depends on Active.
func (q *TurnQueue[W]) MarkActive(chatID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.threads == nil {
		q.threads = map[uuid.UUID]*threadTurns[W]{}
	}
	if _, ok := q.threads[chatID]; !ok {
		q.threads[chatID] = &threadTurns[W]{running: true, wake: make(chan struct{}, 1)}
	}
}

// next takes the thread's oldest batch when it may run now. Otherwise it returns how long
// until it may (0 when nothing waits).
func (q *TurnQueue[W]) next(t *threadTurns[W]) ([]W, time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(t.batches) == 0 {
		return nil, 0
	}
	b := t.batches[0]
	if !b.now {
		if wait := b.since.Add(q.gather()).Sub(q.clock().Now()); wait > 0 {
			return nil, wait
		}
	}
	t.batches = t.batches[1:]
	t.waiting -= len(b.items)
	t.running = true
	return b.items, 0
}

func (q *TurnQueue[W]) work(ctx context.Context, chatID uuid.UUID, t *threadTurns[W]) {
	idleFor := q.Idle
	if idleFor <= 0 {
		idleFor = DefaultWorkerIdle
	}
	for {
		batch, wait := q.next(t)
		if batch != nil {
			q.Run(ctx, batch)
			q.mu.Lock()
			t.running = false
			q.mu.Unlock()
			continue
		}
		idling := wait == 0
		if idling {
			wait = idleFor
		}
		select {
		case <-ctx.Done():
			q.mu.Lock()
			delete(q.threads, chatID)
			q.mu.Unlock()
			return
		case <-t.wake:
		case <-q.clock().After(wait):
			if !idling {
				continue
			}
			q.mu.Lock()
			if t.waiting == 0 {
				delete(q.threads, chatID)
				q.mu.Unlock()
				return
			}
			q.mu.Unlock()
		}
	}
}

// Throttle reports at most once per Every whether an action keyed by id is due. The relays
// use it so a flood of tags cannot make the persona flood the channel back with "busy" notices.
type Throttle struct {
	Every time.Duration

	mu   sync.Mutex
	last map[uuid.UUID]time.Time
}

// Due reports whether id may act now, and records it when so.
func (t *Throttle) Due(id uuid.UUID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[uuid.UUID]time.Time{}
	}
	if at, ok := t.last[id]; ok && time.Since(at) < t.Every {
		return false
	}
	t.last[id] = time.Now()
	return true
}

// SleepCtx waits d or until ctx ends; it reports whether to keep going.
func SleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
