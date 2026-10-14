// Package remotesyncplugin wires remote sync into the API server: the /api/remote-sync routes the
// Integrations "Remote sync" tab uses, and the runner that syncs due accounts. It registers
// itself through internal/plugins and is linked by a blank import in cmd/api-server.
//
// Configuration (environment):
//
//	REMOTE_SYNC_ENABLED=true               turn the feature on (default off)
//	REMOTE_SYNC_INTERVAL=5m                how often an enabled account syncs (Go duration, at least 1m)
//	REMOTE_SYNC_MAX_THREADS_PER_RUN=50     threads synced per run; the rest wait for the next
//	REMOTE_SYNC_ALLOW_LOCAL_NETWORK=true   accept http:// and private or loopback addresses
//	                                       (a server on your own network, or development)
//
// It is off by default because a server with it on makes outbound requests to an address each
// user chooses; that is what a self-hosted install wants and what a shared one should decide on
// purpose. Remote tokens are encrypted at rest with the MCP token key (TOKEN_ENCRYPTION_SECRET).
// See docs/adr/0x026-remote-sync.md.
package remotesyncplugin

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/remotesync"
	"go.uber.org/zap"
)

func init() {
	plugins.Register(apply)
}

// truthy reads a boolean environment variable at apply time (not package init), so a value from
// the server's .env file, loaded by main's init after this package's, is honoured.
func truthy(name string) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(name))) {
	case "true", "1", "on", "yes":
		return true
	}
	return false
}

func apply(d plugins.Deps) {
	if d.Logger == nil {
		return
	}
	logger := d.Logger.Named("remotesync")
	if !truthy("REMOTE_SYNC_ENABLED") {
		logger.Info("remote sync: off (set REMOTE_SYNC_ENABLED=true to turn it on)")
		return
	}
	if d.DataStore == nil || d.AuthRouter == nil || d.Lifecycle == nil {
		logger.Warn("remote sync: missing datastore, router or lifecycle; not started")
		return
	}

	allowLocal := truthy("REMOTE_SYNC_ALLOW_LOCAL_NETWORK")
	cfg := remotesync.Config{
		Interval:          durationEnv(logger, "REMOTE_SYNC_INTERVAL", remotesync.DefaultInterval, time.Minute),
		MaxThreadsPerRun:  intEnv(logger, "REMOTE_SYNC_MAX_THREADS_PER_RUN", remotesync.DefaultMaxThreadsPerRun),
		AllowLocalNetwork: allowLocal,
	}
	syncer := remotesync.NewSyncer(d.DataStore, cfg, logger)
	httpClient := remotesync.NewHTTPClient(allowLocal)

	h := &Handler{
		Store:  d.DataStore,
		Syncer: syncer,
		Check: func(ctx context.Context, baseURL, token string) error {
			c, err := remotesync.NewClient(baseURL, token, httpClient)
			if err != nil {
				return err
			}
			return c.Check(ctx)
		},
		Lifecycle:         d.Lifecycle,
		AllowLocalNetwork: allowLocal,
		Logger:            logger,
	}
	h.Register(d.AuthRouter.PathPrefix("/remote-sync").Subrouter())

	go (&remotesync.Runner{Syncer: syncer, Logger: logger}).Run(d.Lifecycle)
	logger.Info("remote sync: routes registered, runner started",
		zap.Duration("interval", syncer.Interval()), zap.Bool("allow_local_network", allowLocal))
}

func durationEnv(logger *zap.Logger, name string, def, floor time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		logger.Warn("remote sync: ignoring invalid duration", zap.String("var", name), zap.String("value", raw))
		return def
	}
	return max(v, floor)
}

func intEnv(logger *zap.Logger, name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		logger.Warn("remote sync: ignoring invalid number", zap.String("var", name), zap.String("value", raw))
		return def
	}
	return v
}
