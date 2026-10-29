// Package buzzplugin wires the Buzz relay into the API server: the HTTP routes the Integrations
// "Buzz" tab uses, the reply hook that mirrors replies to Buzz, and the gateway that holds each
// persona's connection to each of its communities. It registers itself through
// internal/plugins and is linked by a blank import in cmd/api-server.
//
// Configuration (environment):
//
//	BUZZ_RELAY_ENABLED=true             turn the relay on (default off)
//	BUZZ_GATEWAY_LOCK_KEY=<int64>       advisory-lock key for gateway leadership (default 80920034)
//	BUZZ_GATEWAY_SINGLE_INSTANCE=true   lead without a lock (one process only)
//	BUZZ_ALLOW_PRIVATE_RELAYS=true      allow relays on loopback/private addresses and plain ws/http
//	                                    (local development only)
//
// Personas' secret keys and owner attestations are encrypted at rest with the MCP token key
// (TOKEN_ENCRYPTION_SECRET); without it no persona can be connected. See
// docs/adr/0x028-buzz-relay.md.
package buzzplugin

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

// DefaultLockKey is the advisory lock key for Buzz gateway leadership. Advisory keys share one
// namespace (the registry is in .env.example): 80920031 is the agent-job scheduler, 80920032
// the Discord gateway, 80920033 the memory embedding backfill.
const DefaultLockKey int64 = 80920034

// active is the running relay, set when the plugin is applied. The reply hook is registered at
// init (replyhook requires it) and does nothing until then.
var active atomic.Pointer[buzzrelay.Service]

// unregisterHook removes the reply hook again when the relay turns out to be switched off.
var unregisterHook func()

func init() {
	unregisterHook = replyhook.Register(func(ctx context.Context, ev replyhook.Event) {
		if svc := active.Load(); svc != nil {
			svc.OnReply(ctx, ev)
		}
	})
	plugins.Register(apply)
}

// envFlag reads a boolean environment variable when the plugin is applied (not at package
// init, so a value from the server's .env file counts).
func envFlag(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "true" || v == "1" || v == "on"
}

func apply(d plugins.Deps) {
	if !buzzrelay.Enabled() {
		if unregisterHook != nil {
			unregisterHook()
		}
		if d.Logger != nil {
			d.Logger.Info("buzz relay: off (set BUZZ_RELAY_ENABLED=true to turn it on)")
		}
		return
	}
	if d.DataStore == nil || d.Logger == nil || d.AuthRouter == nil {
		return
	}
	logger := d.Logger.Named("buzz")
	allowPrivate := envFlag("BUZZ_ALLOW_PRIVATE_RELAYS")
	if allowPrivate {
		logger.Warn("buzz relay: BUZZ_ALLOW_PRIVATE_RELAYS is on; personas may connect to private addresses (development only)")
	}
	client := buzzrelay.NewClient(allowPrivate)

	h := &Handler{Store: d.DataStore, Relay: client, Files: d.FileStore, Logger: logger, AllowPrivate: allowPrivate}
	routes := d.AuthRouter.PathPrefix("/buzz").Subrouter()
	h.Register(routes)

	if d.Turns == nil || d.Lifecycle == nil {
		logger.Warn("buzz relay: no turn starter or lifecycle; replies to mentions are disabled")
		return
	}
	svc := &buzzrelay.Service{Store: d.DataStore, Relay: client, Turns: d.Turns, Logger: logger}
	if d.FileStore != nil {
		svc.Files = d.FileStore
	}
	if d.Attachments != nil {
		svc.Ingest = d.Attachments
	}
	active.Store(svc)

	gw := &gateway{client: client, store: d.DataStore, svc: svc, logger: logger}
	sup := &relaykit.Supervisor[models.BuzzConnection]{
		Name:        "buzz gateway",
		Locker:      locker(d, logger),
		List:        gw.list(d.DataStore.ListActiveBuzzConnections),
		Key:         func(c models.BuzzConnection) uuid.UUID { return c.Community.ID },
		Fingerprint: fingerprint,
		Dial:        gw.dial,
		Logger:      logger,
	}
	// Applied during setup, before the server serves, so the handler sees it from the first request.
	h.Changed = sup.Kick
	go sup.Run(d.Lifecycle)
	logger.Info("buzz relay: routes registered, gateway supervisor started")
}

func locker(d plugins.Deps, logger *zap.Logger) relaykit.Locker {
	if envFlag("BUZZ_GATEWAY_SINGLE_INSTANCE") {
		logger.Info("buzz relay: single-instance gateway (no leader lock)")
		return relaykit.SingleInstanceLocker{}
	}
	key := DefaultLockKey
	if v := strings.TrimSpace(os.Getenv("BUZZ_GATEWAY_LOCK_KEY")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			key = parsed
		} else {
			logger.Warn("buzz relay: ignoring invalid BUZZ_GATEWAY_LOCK_KEY", zap.String("value", v))
		}
	}
	return relaykit.AdvisoryLocker{DS: d.DataStore, Key: key}
}

// fingerprint changes when a connection must be reopened: a new key or attestation, or a
// different set of bound channels (the subscriptions are set when connecting; a reopened
// session resumes from the newest event seen before, and redeliveries are deduplicated on the
// event id).
func fingerprint(c models.BuzzConnection) string {
	ids := make([]string, 0, len(c.Channels))
	for _, ch := range c.Channels {
		ids = append(ids, ch.BindingID.String()+"="+ch.ChannelID)
	}
	sort.Strings(ids)
	return c.Agent.PubKey + "|" + c.Community.RelayURL + "|" + c.AuthTag + "|" + strings.Join(ids, ",")
}

// subPrefix names a binding's subscription: "b:<binding id>".
const subPrefix = "b:"

// gateway opens the sessions the supervisor asks for.
type gateway struct {
	client *buzzrelay.Client
	store  stateStore
	svc    *buzzrelay.Service
	logger *zap.Logger
	marks  resumeMarks
}

// stateStore is where the gateway records connection and subscription health, and where it
// resumes from.
type stateStore interface {
	SetBuzzCommunityState(ctx context.Context, id uuid.UUID, status models.BuzzCommunityStatus, lastError string) error
	SetBuzzBindingStatus(ctx context.Context, id uuid.UUID, status models.BuzzBindingStatus, lastError string) error
	MarkBuzzBindingEvent(ctx context.Context, id uuid.UUID, at time.Time) error
	ClearBuzzBindingEvents(ctx context.Context, ids []uuid.UUID) error
}

// maxResume bounds how far back a fresh connection reaches. A channel quiet for days resumes
// from an hour back, and mentions older than that are left rather than answered late.
const maxResume = time.Hour

// markEvery bounds how often a binding's resume point is written. It is shorter than the
// session's skew (a minute), so the events after the last write are still asked for again.
const markEvery = 30 * time.Second

// resumeMarks is where each bound channel's subscription resumes: the newest event seen while
// listening, kept in memory across re-dials (another binding of the community changing reopens
// the connection) and written to the binding row across restarts and leader changes. A
// binding the gateway stops listening to (its community or persona paused, inbound off, the
// binding broken) loses its mark, so listening again starts from now rather than answering
// what was said meanwhile.
type resumeMarks struct {
	mu        sync.Mutex
	seen      map[uuid.UUID]int64
	saved     map[uuid.UUID]time.Time
	listening map[uuid.UUID]bool
	// unsaved are bindings whose stored mark should be gone but could not be cleared yet:
	// their stored mark is ignored, and clearing is retried at the next listing.
	unsaved map[uuid.UUID]bool
}

// start is where binding id's subscription begins: the newer of the stored and the in-memory
// mark, no earlier than maxResume ago; now when there is neither.
func (m *resumeMarks) start(id uuid.UUID, stored *time.Time, now time.Time) time.Time {
	var at int64
	m.mu.Lock()
	if stored != nil && !m.unsaved[id] {
		at = stored.Unix()
	}
	if v := m.seen[id]; v > at {
		at = v
	}
	m.mu.Unlock()
	if at == 0 {
		return now
	}
	t := time.Unix(at, 0)
	if floor := now.Add(-maxResume); t.Before(floor) {
		return floor
	}
	if t.After(now) {
		return now
	}
	return t
}

// note records a delivered event's time and reports whether the mark is due to be written.
// Times more than a minute ahead are ignored, as the session does, so a skewed clock cannot
// push the mark past events not yet seen.
func (m *resumeMarks) note(id uuid.UUID, createdAt int64, now time.Time) bool {
	if createdAt > now.Add(time.Minute).Unix() {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// An event from a session still closing after its binding left the listing must not bring
	// the mark back.
	if !m.listening[id] {
		return false
	}
	if m.seen == nil {
		m.seen, m.saved = map[uuid.UUID]int64{}, map[uuid.UUID]time.Time{}
	}
	if createdAt <= m.seen[id] {
		return false
	}
	m.seen[id] = createdAt
	if now.Sub(m.saved[id]) < markEvery {
		return false
	}
	m.saved[id] = now
	return true
}

// listed takes the bindings the gateway listens to now and returns the ones whose stored marks
// must be cleared: those it listened to at the previous listing and no longer does (their
// in-memory marks are forgotten here), plus earlier ones whose clearing failed.
func (m *resumeMarks) listed(ids []uuid.UUID) (stale []uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		now[id] = true
	}
	if m.unsaved == nil {
		m.unsaved = map[uuid.UUID]bool{}
	}
	for id := range m.listening {
		if !now[id] {
			m.unsaved[id] = true
			delete(m.seen, id)
			delete(m.saved, id)
		}
	}
	m.listening = now
	for id := range m.unsaved {
		stale = append(stale, id)
	}
	return stale
}

// cleared records that the stored marks of ids are gone.
func (m *resumeMarks) cleared(ids []uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.unsaved, id)
	}
}

// list wraps the gateway's work list: bindings that dropped out of it since the last listing
// lose their resume points, here and on their rows.
func (g *gateway) list(next func(context.Context) ([]models.BuzzConnection, error)) func(context.Context) ([]models.BuzzConnection, error) {
	return func(ctx context.Context) ([]models.BuzzConnection, error) {
		conns, err := next(ctx)
		if err != nil {
			return nil, err
		}
		var ids []uuid.UUID
		for _, c := range conns {
			for _, ch := range c.Channels {
				ids = append(ids, ch.BindingID)
			}
		}
		if stale := g.marks.listed(ids); len(stale) > 0 {
			if err := g.store.ClearBuzzBindingEvents(ctx, stale); err != nil {
				// Kept as unsaved: their stored marks are ignored until a later listing clears them.
				g.logger.Warn("buzz gateway: forget resume points; retrying at the next listing", zap.Error(err))
			} else {
				g.marks.cleared(stale)
			}
		}
		return conns, nil
	}
}

func (g *gateway) dial(ctx context.Context, c models.BuzzConnection) (relaykit.Conn, error) {
	ep, signer, err := endpointSigner(&c)
	if err != nil {
		return nil, err
	}
	subs := map[string]buzzrelay.Filter{}
	now := time.Now()
	start := now
	for _, ch := range c.Channels {
		from := g.marks.start(ch.BindingID, ch.LastEventAt, now)
		if from.Before(start) {
			start = from
		}
		subs[subPrefix+ch.BindingID.String()] = buzzrelay.Filter{
			"kinds": []int{buzzrelay.KindStreamMessage},
			"#h":    []string{ch.ChannelID},
			// Each channel from its own mark, minus the session's skew. The session's own
			// bound (Start, then the newest event seen) takes over when it is later.
			"since": from.Add(-time.Minute).Unix(),
		}
	}
	log := g.logger.With(zap.String("community_id", c.Community.ID.String()), zap.String("relay", c.Community.RelayURL))
	communityID := c.Community.ID
	personaPub := c.Agent.PubKey
	session := g.client.Open(ctx, buzzrelay.SessionConfig{
		Endpoint: ep,
		Signer:   signer,
		Subs:     subs,
		Start:    start,
		Logger:   log,
		OnEvent: func(subID string, ev nostr.Event) {
			id, err := uuid.Parse(strings.TrimPrefix(subID, subPrefix))
			if err != nil {
				return
			}
			if g.marks.note(id, ev.CreatedAt, time.Now()) {
				at := time.Unix(ev.CreatedAt, 0)
				go func() {
					if err := g.store.MarkBuzzBindingEvent(context.WithoutCancel(ctx), id, at); err != nil {
						log.Warn("buzz gateway: record resume point", zap.Error(err))
					}
				}()
			}
			// Most of a channel's traffic is not for the persona: drop it here, before a
			// goroutine and a binding lookup (with key decryption) are spent on it.
			if !buzzrelay.MayConcern(ev, personaPub) {
				return
			}
			go g.svc.HandleEvent(ctx, id, ev)
		},
		OnState: func(st buzzrelay.SessionState, detail string) {
			status := models.BuzzCommunityError
			switch st {
			case buzzrelay.SessionActive:
				status = models.BuzzCommunityActive
			case buzzrelay.SessionNeedsAccess:
				status = models.BuzzCommunityNeedsAccess
			}
			if err := g.store.SetBuzzCommunityState(context.WithoutCancel(ctx), communityID, status, detail); err != nil {
				log.Warn("buzz gateway: record connection state", zap.Error(err))
			}
		},
		OnClosed: func(subID, reason string) {
			id, err := uuid.Parse(strings.TrimPrefix(subID, subPrefix))
			if err != nil {
				return
			}
			log.Warn("buzz gateway: the relay closed a channel subscription", zap.String("binding_id", id.String()), zap.String("reason", reason))
			if buzzrelay.IsAccessReason(reason) {
				_ = g.store.SetBuzzBindingStatus(context.WithoutCancel(ctx), id, models.BuzzBindingBroken,
					"The persona cannot read this channel ("+reason+"). Add it to the channel in Buzz, then reactivate.")
			}
		},
	})
	return session, nil
}
