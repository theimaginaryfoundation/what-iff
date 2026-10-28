// Package slackplugin wires the Slack relay into the API server: the HTTP routes the
// Integrations "Slack" tab uses, the reply hook that mirrors replies to Slack, and the gateway
// that holds each persona app's Socket Mode connection. It registers itself through
// internal/plugins and is linked by a blank import in cmd/api-server.
//
// Configuration (environment):
//
//	SLACK_RELAY_ENABLED=true             turn the experimental feature on (default off)
//	SLACK_GATEWAY_LOCK_KEY=<int64>       advisory-lock key for gateway leadership (default 80920035)
//	SLACK_GATEWAY_SINGLE_INSTANCE=true   lead without a lock (one process only)
//
// Apps' bot and app-level tokens are encrypted at rest with the MCP token key
// (TOKEN_ENCRYPTION_SECRET); without it no app can be added. See docs/adr/0x032-slack-relay.md.
package slackplugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"github.com/theimaginaryfoundation/what-iff/internal/slackrelay"
	"go.uber.org/zap"
)

// DefaultLockKey is the advisory lock key for Slack gateway leadership. Advisory keys share one
// namespace (the registry is in .env.example): 80920031 is the agent-job scheduler, 80920032
// the Discord gateway, 80920033 the memory embedding backfill, 80920034 the Buzz gateway.
const DefaultLockKey int64 = 80920035

// active is the running relay, set when the plugin is applied. The reply hook is registered at
// init (replyhook requires it) and does nothing until then.
var active atomic.Pointer[slackrelay.Service]

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
	if !slackrelay.Enabled() {
		if unregisterHook != nil {
			unregisterHook()
		}
		if d.Logger != nil {
			d.Logger.Info("slack relay: off (experimental; set SLACK_RELAY_ENABLED=true to turn it on)")
		}
		return
	}
	if d.DataStore == nil || d.Logger == nil || d.AuthRouter == nil {
		return
	}
	logger := d.Logger.Named("slack")
	api := slackrelay.NewAPI()
	dialer := slackrelay.NewDialer()

	h := &Handler{Store: d.DataStore, Slack: api, Logger: logger,
		CheckAppToken: func(ctx context.Context, appToken string) (string, error) {
			return slackrelay.SocketAppID(ctx, api, dialer, appToken)
		}}
	routes := d.AuthRouter.PathPrefix("/slack").Subrouter()
	routes.Use(experimentalOnly(logger))
	h.Register(routes)

	if d.Turns == nil || d.Lifecycle == nil {
		logger.Warn("slack relay: no turn starter or lifecycle; replies to mentions are disabled")
		return
	}
	svc := &slackrelay.Service{Store: d.DataStore, Slack: api, Turns: d.Turns, Logger: logger, Policy: relaykit.PolicyFromEnv(logger)}
	if d.FileStore != nil {
		svc.Files = d.FileStore
	}
	if d.Attachments != nil {
		svc.Ingest = d.Attachments
	}
	active.Store(svc)

	gw := &gateway{opener: api, dialer: dialer, store: d.DataStore, events: svc, logger: logger}
	sup := &relaykit.Supervisor[models.SlackAppCredentials]{
		Name:        "slack gateway",
		Locker:      locker(d, logger),
		List:        mayUse(d.DataStore.ListActiveSlackAppCredentials),
		Key:         func(c models.SlackAppCredentials) uuid.UUID { return c.ID },
		Fingerprint: fingerprint,
		Dial:        gw.dial,
		Logger:      logger,
	}
	// Applied during setup, before the server serves, so the handler sees it from the first request.
	h.Changed = sup.Kick
	go sup.Run(d.Lifecycle)
	logger.Info("slack relay: routes registered, gateway supervisor started")
}

func locker(d plugins.Deps, logger *zap.Logger) relaykit.Locker {
	if envFlag("SLACK_GATEWAY_SINGLE_INSTANCE") {
		logger.Info("slack relay: single-instance gateway (no leader lock)")
		return relaykit.SingleInstanceLocker{}
	}
	key := DefaultLockKey
	if v := strings.TrimSpace(os.Getenv("SLACK_GATEWAY_LOCK_KEY")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			key = parsed
		} else {
			logger.Warn("slack relay: ignoring invalid SLACK_GATEWAY_LOCK_KEY", zap.String("value", v))
		}
	}
	return relaykit.AdvisoryLocker{DS: d.DataStore, Key: key}
}

// fingerprint changes when a connection must be reopened: new tokens. Bindings do not count:
// Slack sends an app every message of every channel it is in, and the relay looks the binding
// up per event. The tokens are hashed so they never sit in the supervisor's bookkeeping.
func fingerprint(c models.SlackAppCredentials) string {
	sum := sha256.Sum256([]byte(c.AppToken + "\x00" + c.BotToken + "\x00" + c.BotUserID))
	return hex.EncodeToString(sum[:])
}

// experimentalOnly answers 404 on every /api/slack route for a user the per-user experimental
// policy has the relay off for, exactly as when the relay is switched off, so the web app hides
// its Slack surfaces for them.
func experimentalOnly(logger *zap.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := middleware.GetUserIDFromContext(r.Context())
			if !ok || !slackrelay.UserMayUse(r.Context(), userID) {
				handlerutils.RespondWithError(w, logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// mayUse keeps the gateway to the apps of owners the relay is on for, so turning the feature
// off for a user closes their apps' connections at the next reconcile.
func mayUse(list func(context.Context) ([]models.SlackAppCredentials, error)) func(context.Context) ([]models.SlackAppCredentials, error) {
	return func(ctx context.Context) ([]models.SlackAppCredentials, error) {
		all, err := list(ctx)
		if err != nil {
			return nil, err
		}
		allowed := map[uuid.UUID]bool{}
		out := all[:0]
		for _, c := range all {
			ok, seen := allowed[c.OwnerID]
			if !seen {
				ok = slackrelay.UserMayUse(ctx, c.OwnerID)
				allowed[c.OwnerID] = ok
			}
			if ok {
				out = append(out, c)
			}
		}
		return out, nil
	}
}
