// Package gateway holds the Discord Gateway connections for every active bot.
//
// Exactly one process may hold a bot's connection, or every API task would
// answer every mention. The supervisor therefore runs only while it holds a
// leader lock (the same Postgres advisory lock mechanism the agent-job
// scheduler uses, on its own key). The leader opens one connection per active
// bot and reconciles the set periodically; the other tasks wait to take over.
//
// Nothing here depends on running inside the API server: Run takes a lock, a
// bot source and a sink. In-process, the sink calls the relay directly; split
// into its own service, the same Run would POST to an internal route.
package gateway

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"go.uber.org/zap"
)

// Bot is what the supervisor needs to connect one bot.
type Bot struct {
	ID        uuid.UUID
	Token     string
	BotUserID string
	// MessageContent is true when the bot's application has the privileged
	// Message Content intent switched on, so it may be requested.
	MessageContent bool
}

// fingerprint changes when a reconnect is needed (a new token or intent set).
func (b Bot) fingerprint() string {
	if b.MessageContent {
		return b.Token + "|content"
	}
	return b.Token
}

// BotSource lists the bots that should be connected right now.
type BotSource interface {
	ActiveBots(ctx context.Context) ([]Bot, error)
}

// Sink receives a message seen by a bot. It is called on the connection's event
// goroutine, so it must return quickly (hand work off) and be safe for
// concurrent use across bots.
type Sink interface {
	HandleInbound(ctx context.Context, bot Bot, msg discordrelay.InboundMessage)
}

// Lock is a held leader lock.
type Lock interface {
	IsHealthy(ctx context.Context) (bool, error)
	Release(ctx context.Context) error
}

// Locker tries, without blocking, to become leader.
type Locker interface {
	TryAcquire(ctx context.Context) (Lock, bool, error)
}

// Conn is one open bot connection.
type Conn interface {
	Close() error
}

// Dialer opens a connection for bot and delivers its messages to onMessage.
type Dialer func(ctx context.Context, bot Bot, onMessage func(discordrelay.InboundMessage)) (Conn, error)

// Supervisor keeps the connection set in line with BotSource while it leads.
type Supervisor struct {
	Locker Locker
	Source BotSource
	Sink   Sink
	Dial   Dialer
	Logger *zap.Logger

	// ReconcileEvery is how often the leader re-reads the bot list and checks its
	// lock. RetryEvery is how often a follower tries to take the lock. Both get
	// jitter so tasks do not move in lockstep.
	ReconcileEvery time.Duration
	RetryEvery     time.Duration

	mu    sync.Mutex
	conns map[uuid.UUID]openConn
}

type openConn struct {
	conn        Conn
	fingerprint string
}

// Defaults for the intervals when zero.
const (
	DefaultReconcileEvery = 30 * time.Second
	DefaultRetryEvery     = 15 * time.Second
)

// Run blocks until ctx is cancelled, leading whenever it can. On return every
// connection is closed and the lock released.
func (s *Supervisor) Run(ctx context.Context) {
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
			s.Logger.Warn("discord gateway: leader lock attempt failed", zap.Error(err))
		case ok:
			s.Logger.Info("discord gateway: leading; connecting bots")
			s.lead(ctx, lock)
			s.Logger.Info("discord gateway: no longer leading")
		}
		if !sleep(ctx, jitter(s.RetryEvery)) {
			return
		}
	}
}

// lead runs while lock is healthy, reconciling connections.
func (s *Supervisor) lead(ctx context.Context, lock Lock) {
	defer func() {
		s.closeAll()
		// Release on a fresh context: ctx may already be cancelled (shutdown),
		// and releasing promptly lets another task take over without waiting
		// for the session to time out.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			s.Logger.Warn("discord gateway: release leader lock", zap.Error(err))
		}
	}()
	for {
		healthy, err := lock.IsHealthy(ctx)
		if err != nil || !healthy {
			if ctx.Err() == nil {
				s.Logger.Warn("discord gateway: leader lock lost", zap.Error(err))
			}
			return
		}
		s.Reconcile(ctx)
		if !sleep(ctx, jitter(s.ReconcileEvery)) {
			return
		}
	}
}

// Reconcile opens connections for new bots, reopens ones whose token or intents
// changed, and closes ones that are no longer active. A failure to list bots
// leaves the current connections alone.
func (s *Supervisor) Reconcile(ctx context.Context) {
	bots, err := s.Source.ActiveBots(ctx)
	if err != nil {
		s.Logger.Warn("discord gateway: list active bots", zap.Error(err))
		return
	}
	want := make(map[uuid.UUID]Bot, len(bots))
	for _, b := range bots {
		want[b.ID] = b
	}

	s.mu.Lock()
	if s.conns == nil {
		s.conns = make(map[uuid.UUID]openConn)
	}
	var toClose []Conn
	for id, oc := range s.conns {
		if b, ok := want[id]; !ok || b.fingerprint() != oc.fingerprint {
			toClose = append(toClose, oc.conn)
			delete(s.conns, id)
		}
	}
	var toOpen []Bot
	for id, b := range want {
		if _, ok := s.conns[id]; !ok {
			toOpen = append(toOpen, b)
		}
	}
	s.mu.Unlock()

	for _, c := range toClose {
		_ = c.Close()
	}
	for _, b := range toOpen {
		bot := b
		conn, err := s.Dial(ctx, bot, func(msg discordrelay.InboundMessage) {
			s.Sink.HandleInbound(ctx, bot, msg)
		})
		if err != nil {
			s.Logger.Warn("discord gateway: connect bot", zap.String("bot_id", bot.ID.String()), zap.Error(err))
			continue
		}
		s.mu.Lock()
		s.conns[bot.ID] = openConn{conn: conn, fingerprint: bot.fingerprint()}
		s.mu.Unlock()
		s.Logger.Info("discord gateway: bot connected", zap.String("bot_id", bot.ID.String()))
	}
}

// Connected returns the ids of the bots with an open connection.
func (s *Supervisor) Connected() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uuid.UUID, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	return ids
}

func (s *Supervisor) closeAll() {
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

// sleep waits d or until ctx ends; it reports whether to keep going.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
