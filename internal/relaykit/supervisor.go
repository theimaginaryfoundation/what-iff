package relaykit

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// Lock is a held leader lock.
type Lock interface {
	IsHealthy(ctx context.Context) (bool, error)
	Release(ctx context.Context) error
}

// Locker tries, without blocking, to become leader.
type Locker interface {
	TryAcquire(ctx context.Context) (Lock, bool, error)
}

// AdvisoryLocker leads through the datastore's Postgres advisory lock, the same mechanism
// the agent-job scheduler uses for its leadership. Each relay uses its own key.
type AdvisoryLocker struct {
	DS  *datastore.Datastore
	Key int64
}

func (l AdvisoryLocker) TryAcquire(ctx context.Context) (Lock, bool, error) {
	lock, ok, err := l.DS.TryAcquireSchedulerLeaderLock(ctx, l.Key)
	if err != nil || !ok {
		return nil, ok, err
	}
	return lock, true, nil
}

// SingleInstanceLocker always leads. For a single-process deployment, or a local database
// without advisory locks (sqlite); never with more than one API task.
type SingleInstanceLocker struct{}

func (SingleInstanceLocker) TryAcquire(context.Context) (Lock, bool, error) {
	return alwaysHealthy{}, true, nil
}

type alwaysHealthy struct{}

func (alwaysHealthy) IsHealthy(ctx context.Context) (bool, error) { return ctx.Err() == nil, nil }
func (alwaysHealthy) Release(context.Context) error               { return nil }

// Conn is one open long-lived connection.
type Conn interface {
	Close() error
}

// Defaults for the supervisor's intervals when zero.
const (
	DefaultReconcileEvery = 30 * time.Second
	DefaultRetryEvery     = 15 * time.Second
)

// Supervisor keeps one connection per target open while it holds a leader lock, so that
// exactly one API task holds each connection (otherwise every task would answer every tag).
// The leader re-lists the targets periodically and opens, reopens (when a target's
// fingerprint changes: a new token, new subscriptions) or closes connections to match;
// the other tasks wait to take over.
//
// Nothing here depends on running inside the API server: split into its own service, the
// same Run would deliver to an internal route instead of calling the relay directly.
type Supervisor[T any] struct {
	// Name prefixes log lines ("discord gateway").
	Name   string
	Locker Locker
	// List returns the targets that should be connected right now.
	List func(ctx context.Context) ([]T, error)
	// Key identifies a target; Fingerprint changes when it must be reconnected.
	Key         func(T) uuid.UUID
	Fingerprint func(T) string
	// Dial opens a target's connection. It must return once the connection is set up and
	// deliver what it receives on its own goroutines.
	Dial   func(ctx context.Context, t T) (Conn, error)
	Logger *zap.Logger

	// ReconcileEvery is how often the leader re-reads the target list and checks its lock.
	// RetryEvery is how often a follower tries to take the lock. Both get jitter so tasks
	// do not move in lockstep.
	ReconcileEvery time.Duration
	RetryEvery     time.Duration

	mu    sync.Mutex
	conns map[uuid.UUID]openConn
	kick  chan struct{}
	once  sync.Once
}

func (s *Supervisor[T]) kicks() chan struct{} {
	s.once.Do(func() { s.kick = make(chan struct{}, 1) })
	return s.kick
}

// Kick asks the leader to reconcile now rather than at its next interval (after the owner
// bound a channel, say). It never blocks; on a follower it does nothing until it leads, and
// with several API tasks only the leader's own kicks reach it (the others still converge at
// the next interval).
func (s *Supervisor[T]) Kick() {
	select {
	case s.kicks() <- struct{}{}:
	default:
	}
}

type openConn struct {
	conn        Conn
	fingerprint string
}

// Run blocks until ctx is cancelled, leading whenever it can. On return every connection is
// closed and the lock released.
func (s *Supervisor[T]) Run(ctx context.Context) {
	if s.ReconcileEvery <= 0 {
		s.ReconcileEvery = DefaultReconcileEvery
	}
	if s.RetryEvery <= 0 {
		s.RetryEvery = DefaultRetryEvery
	}
	for {
		lock, ok, err := s.Locker.TryAcquire(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.Logger.Warn(s.Name+": leader lock attempt failed", zap.Error(err))
		case ok:
			s.Logger.Info(s.Name + ": leading; connecting")
			s.lead(ctx, lock)
			s.Logger.Info(s.Name + ": no longer leading")
		}
		if !SleepCtx(ctx, jitter(s.RetryEvery)) {
			return
		}
	}
}

// lead runs while lock is healthy, reconciling connections.
func (s *Supervisor[T]) lead(ctx context.Context, lock Lock) {
	defer func() {
		s.closeAll()
		// Release on a fresh context: ctx may already be cancelled (shutdown), and
		// releasing promptly lets another task take over without waiting for the
		// session to time out.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			s.Logger.Warn(s.Name+": release leader lock", zap.Error(err))
		}
	}()
	for {
		healthy, err := lock.IsHealthy(ctx)
		if err != nil || !healthy {
			if ctx.Err() == nil {
				s.Logger.Warn(s.Name+": leader lock lost", zap.Error(err))
			}
			return
		}
		s.Reconcile(ctx)
		t := time.NewTimer(jitter(s.ReconcileEvery))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-s.kicks():
			t.Stop()
		}
	}
}

// Reconcile opens connections for new targets, reopens ones whose fingerprint changed, and
// closes ones no longer listed. A failure to list leaves the current connections alone.
func (s *Supervisor[T]) Reconcile(ctx context.Context) {
	targets, err := s.List(ctx)
	if err != nil {
		s.Logger.Warn(s.Name+": list connection targets", zap.Error(err))
		return
	}
	want := make(map[uuid.UUID]T, len(targets))
	for _, t := range targets {
		want[s.Key(t)] = t
	}

	s.mu.Lock()
	if s.conns == nil {
		s.conns = make(map[uuid.UUID]openConn)
	}
	var toClose []Conn
	for id, oc := range s.conns {
		if t, ok := want[id]; !ok || s.Fingerprint(t) != oc.fingerprint {
			toClose = append(toClose, oc.conn)
			delete(s.conns, id)
		}
	}
	var toOpen []T
	for id, t := range want {
		if _, ok := s.conns[id]; !ok {
			toOpen = append(toOpen, t)
		}
	}
	s.mu.Unlock()

	for _, c := range toClose {
		_ = c.Close()
	}
	for _, t := range toOpen {
		id := s.Key(t)
		conn, err := s.Dial(ctx, t)
		if err != nil {
			s.Logger.Warn(s.Name+": connect", zap.String("id", id.String()), zap.Error(err))
			continue
		}
		s.mu.Lock()
		s.conns[id] = openConn{conn: conn, fingerprint: s.Fingerprint(t)}
		s.mu.Unlock()
		s.Logger.Info(s.Name+": connected", zap.String("id", id.String()))
	}
}

// Connected returns the keys of the targets with an open connection.
func (s *Supervisor[T]) Connected() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uuid.UUID, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	return ids
}

func (s *Supervisor[T]) closeAll() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, oc := range conns {
		_ = oc.conn.Close()
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d + time.Duration(rand.Int64N(int64(d)/5+1))
}
