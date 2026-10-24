// Package relaykit is the machinery the chat relays (Discord, Buzz) share: one turn at a
// time per relay thread with a bounded queue, the bookkeeping that matches a finished reply
// to the tag that asked for it, a leader-locked supervisor for long-lived connections, the
// public-address guard for outbound connections, and the speaker-label and allow-list rules.
// See docs/adr/0x025-discord-relay.md and docs/adr/0x028-buzz-relay.md.
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

// TurnQueue runs work one item at a time per relay thread, with at most Depth items waiting
// behind the one running. Each thread gets a worker on first use, which exits once its queue
// has been idle, so idle threads hold nothing.
type TurnQueue[W any] struct {
	// Depth bounds the items waiting per thread (5 when zero).
	Depth int
	// Idle is how long a worker waits before exiting (DefaultWorkerIdle when zero).
	Idle time.Duration
	// Run handles one item. It runs on the thread's worker, so items of one thread never overlap.
	Run func(ctx context.Context, w W)

	mu     sync.Mutex
	queues map[uuid.UUID]chan W
}

// Enqueue adds w to the thread's queue, starting its worker on first use, and reports false
// when the queue is full. The send happens under the lock the worker takes before it retires
// an idle queue, so an item can never land in a queue whose worker has just exited.
func (q *TurnQueue[W]) Enqueue(ctx context.Context, chatID uuid.UUID, w W) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queues == nil {
		q.queues = map[uuid.UUID]chan W{}
	}
	ch, ok := q.queues[chatID]
	if !ok {
		depth := q.Depth
		if depth <= 0 {
			depth = 5
		}
		ch = make(chan W, depth)
		q.queues[chatID] = ch
		go q.work(ctx, chatID, ch)
	}
	select {
	case ch <- w:
		return true
	default:
		return false
	}
}

// Active reports whether the thread has a worker (a turn running or recently run).
func (q *TurnQueue[W]) Active(chatID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.queues[chatID]
	return ok
}

// MarkActive registers an idle queue for the thread without a worker, as if a turn were
// running there. It exists for tests of what depends on Active.
func (q *TurnQueue[W]) MarkActive(chatID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queues == nil {
		q.queues = map[uuid.UUID]chan W{}
	}
	if _, ok := q.queues[chatID]; !ok {
		q.queues[chatID] = make(chan W, 1)
	}
}

func (q *TurnQueue[W]) work(ctx context.Context, chatID uuid.UUID, ch chan W) {
	idleFor := q.Idle
	if idleFor <= 0 {
		idleFor = DefaultWorkerIdle
	}
	idle := time.NewTimer(idleFor)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case w := <-ch:
			q.Run(ctx, w)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleFor)
		case <-idle.C:
			q.mu.Lock()
			if len(ch) == 0 {
				delete(q.queues, chatID)
				q.mu.Unlock()
				return
			}
			q.mu.Unlock()
			idle.Reset(idleFor)
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
