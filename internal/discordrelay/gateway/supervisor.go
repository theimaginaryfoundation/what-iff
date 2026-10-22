// Package gateway holds the Discord Gateway connections for every active bot.
//
// Exactly one process may hold a bot's connection, or every API task would
// answer every mention. The supervisor (relaykit.Supervisor) therefore runs only
// while it holds a leader lock (the same Postgres advisory lock mechanism the
// agent-job scheduler uses, on its own key). The leader opens one connection per
// active bot and reconciles the set periodically; the other tasks wait to take over.
//
// Nothing here depends on running inside the API server: Run takes a lock, a
// bot source and a sink. In-process, the sink calls the relay directly; split
// into its own service, the same Run would POST to an internal route.
package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
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

// Lock, Locker and Conn are relaykit's.
type (
	Lock   = relaykit.Lock
	Locker = relaykit.Locker
	Conn   = relaykit.Conn
)

// Dialer opens a connection for bot and delivers its messages to onMessage.
type Dialer func(ctx context.Context, bot Bot, onMessage func(discordrelay.InboundMessage)) (Conn, error)

// Supervisor keeps the connection set in line with BotSource while it leads. It is the
// Discord face of relaykit.Supervisor: one connection per active bot, reopened when the
// bot's token or intents change.
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

	once sync.Once
	sup  *relaykit.Supervisor[Bot]
}

// Defaults for the intervals when zero.
const (
	DefaultReconcileEvery = relaykit.DefaultReconcileEvery
	DefaultRetryEvery     = relaykit.DefaultRetryEvery
)

func (s *Supervisor) inner() *relaykit.Supervisor[Bot] {
	s.once.Do(func() {
		s.sup = &relaykit.Supervisor[Bot]{
			Name:   "discord gateway",
			Locker: s.Locker,
			List:   s.Source.ActiveBots,
			Key:    func(b Bot) uuid.UUID { return b.ID },
			Fingerprint: func(b Bot) string {
				return b.fingerprint()
			},
			Dial: func(ctx context.Context, bot Bot) (relaykit.Conn, error) {
				return s.Dial(ctx, bot, func(msg discordrelay.InboundMessage) {
					s.Sink.HandleInbound(ctx, bot, msg)
				})
			},
			Logger:         s.Logger,
			ReconcileEvery: s.ReconcileEvery,
			RetryEvery:     s.RetryEvery,
		}
	})
	return s.sup
}

// Run blocks until ctx is cancelled, leading whenever it can. On return every
// connection is closed and the lock released.
func (s *Supervisor) Run(ctx context.Context) { s.inner().Run(ctx) }

// Reconcile opens connections for new bots, reopens ones whose token or intents
// changed, and closes ones that are no longer active. A failure to list bots
// leaves the current connections alone.
func (s *Supervisor) Reconcile(ctx context.Context) { s.inner().Reconcile(ctx) }

// Connected returns the ids of the bots with an open connection.
func (s *Supervisor) Connected() []uuid.UUID { return s.inner().Connected() }
