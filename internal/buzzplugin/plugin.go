// Package buzzplugin wires the Buzz relay into the API server: the HTTP routes the Integrations
// "Buzz" tab uses, the reply hook that mirrors replies to Buzz, and the gateway that holds each
// persona's connection to each of its communities. It registers itself through
// internal/plugins and is linked by a blank import in cmd/api-server.
//
// Configuration (environment):
//
//	BUZZ_RELAY_ENABLED=true             turn the experimental feature on (default off)
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
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
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
			d.Logger.Info("buzz relay: off (experimental; set BUZZ_RELAY_ENABLED=true to turn it on)")
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
	routes.Use(experimentalOnly(logger))
	h.Register(routes)

	if d.Turns == nil || d.Lifecycle == nil {
		logger.Warn("buzz relay: no turn starter or lifecycle; replies to mentions are disabled")
		return
	}
	svc := &buzzrelay.Service{Store: d.DataStore, Relay: client, Turns: d.Turns, Logger: logger, Policy: relaykit.PolicyFromEnv(logger)}
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
		List:        mayUse(d.DataStore.ListActiveBuzzConnections),
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
// session resumes from a minute back, and redeliveries are deduplicated on the event id).
func fingerprint(c models.BuzzConnection) string {
	ids := make([]string, 0, len(c.Channels))
	for _, ch := range c.Channels {
		ids = append(ids, ch.BindingID.String()+"="+ch.ChannelID)
	}
	sort.Strings(ids)
	return c.Agent.PubKey + "|" + c.Community.RelayURL + "|" + c.AuthTag + "|" + strings.Join(ids, ",")
}

// experimentalOnly answers 404 on every /api/buzz route for a user the per-user experimental
// policy has the relay off for, exactly as when the relay is switched off, so the web app hides
// its Buzz surfaces for them.
func experimentalOnly(logger *zap.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := middleware.GetUserIDFromContext(r.Context())
			if !ok || !buzzrelay.UserMayUse(r.Context(), userID) {
				handlerutils.RespondWithError(w, logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// mayUse keeps the gateway to the connections of owners the relay is on for, so turning the
// feature off for a user closes their personas' connections at the next reconcile.
func mayUse(list func(context.Context) ([]models.BuzzConnection, error)) func(context.Context) ([]models.BuzzConnection, error) {
	return func(ctx context.Context) ([]models.BuzzConnection, error) {
		all, err := list(ctx)
		if err != nil {
			return nil, err
		}
		allowed := map[uuid.UUID]bool{}
		out := all[:0]
		for _, c := range all {
			owner := c.Agent.OwnerID
			ok, seen := allowed[owner]
			if !seen {
				ok = buzzrelay.UserMayUse(ctx, owner)
				allowed[owner] = ok
			}
			if ok {
				out = append(out, c)
			}
		}
		return out, nil
	}
}

// subPrefix names a binding's subscription: "b:<binding id>".
const subPrefix = "b:"

// gateway opens the sessions the supervisor asks for.
type gateway struct {
	client *buzzrelay.Client
	store  stateStore
	svc    *buzzrelay.Service
	logger *zap.Logger
}

// stateStore is where the gateway records connection and subscription health.
type stateStore interface {
	SetBuzzCommunityState(ctx context.Context, id uuid.UUID, status models.BuzzCommunityStatus, lastError string) error
	SetBuzzBindingStatus(ctx context.Context, id uuid.UUID, status models.BuzzBindingStatus, lastError string) error
}

func (g *gateway) dial(ctx context.Context, c models.BuzzConnection) (relaykit.Conn, error) {
	ep, signer, err := endpointSigner(&c)
	if err != nil {
		return nil, err
	}
	subs := map[string]buzzrelay.Filter{}
	start := time.Now()
	for _, ch := range c.Channels {
		subs[subPrefix+ch.BindingID.String()] = buzzrelay.Filter{
			"kinds": []int{buzzrelay.KindStreamMessage},
			"#h":    []string{ch.ChannelID},
		}
	}
	log := g.logger.With(zap.String("community_id", c.Community.ID.String()), zap.String("relay", c.Community.RelayURL))
	communityID := c.Community.ID
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
