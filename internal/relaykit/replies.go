package relaykit

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
)

// Replies matches finished replies (the reply hook) with the inbound turns waiting for them.
//
// A relay turn saves the tag as a user message, then waits for the reply to it to be posted
// before the thread's next tag runs. Two races are handled here:
//   - the reply can finish before the turn starter returns and the waiter is registered
//     (Release before Wait), so an early release is remembered for a while;
//   - the reply can finish before the relay has attached its inbound link to the saved
//     message, so the reply hook finds nothing to post to. It keeps the reply as an orphan,
//     and the relay posts it once the link is attached.
type Replies struct {
	// TTL bounds how long an early release or an orphan is kept.
	TTL time.Duration

	mu      sync.Mutex
	waiting map[uuid.UUID]chan struct{}
	early   map[uuid.UUID]time.Time
	orphans map[uuid.UUID]orphan
}

type orphan struct {
	ev replyhook.Event
	at time.Time
}

// Wait registers ch to be closed when the reply to user message id is posted. If that
// already happened, ch is closed at once.
func (r *Replies) Wait(id uuid.UUID, ch chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, at := range r.early {
		if time.Since(at) > r.TTL {
			delete(r.early, k)
		}
	}
	if _, ok := r.early[id]; ok {
		delete(r.early, id)
		close(ch)
		return
	}
	if r.waiting == nil {
		r.waiting = map[uuid.UUID]chan struct{}{}
	}
	r.waiting[id] = ch
}

// Forget removes the waiter for id, if any.
func (r *Replies) Forget(id uuid.UUID) {
	r.mu.Lock()
	delete(r.waiting, id)
	r.mu.Unlock()
}

// Release marks the reply to id as posted: its waiter is closed, or, when none is registered
// yet, the release is remembered for Wait.
func (r *Replies) Release(id uuid.UUID) {
	r.mu.Lock()
	ch, ok := r.waiting[id]
	delete(r.waiting, id)
	if !ok {
		if r.early == nil {
			r.early = map[uuid.UUID]time.Time{}
		}
		r.early[id] = time.Now()
	}
	r.mu.Unlock()
	if ok {
		close(ch)
	}
}

// KeepOrphan remembers a reply whose trigger has no inbound link yet. The caller decides
// whether a relay turn is running in the chat (only then is a reply worth keeping).
func (r *Replies) KeepOrphan(ev replyhook.Event) {
	if ev.TriggerMessageID == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.orphans == nil {
		r.orphans = map[uuid.UUID]orphan{}
	}
	for k, o := range r.orphans {
		if time.Since(o.at) > r.TTL {
			delete(r.orphans, k)
		}
	}
	r.orphans[*ev.TriggerMessageID] = orphan{ev: ev, at: time.Now()}
}

// TakeOrphan returns and forgets the reply kept for user message id, if any.
func (r *Replies) TakeOrphan(id uuid.UUID) (replyhook.Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orphans[id]
	if ok {
		delete(r.orphans, id)
	}
	return o.ev, ok
}
