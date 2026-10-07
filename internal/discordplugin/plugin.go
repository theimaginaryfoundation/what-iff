// Package discordplugin wires the Discord relay into the API server: the HTTP
// routes the Integrations "Discord" tab uses, the reply hook that mirrors replies
// to Discord, and the gateway supervisor that listens for tags. It registers
// itself through internal/plugins and is linked by a blank import in
// cmd/api-server.
//
// Configuration (environment):
//
//	DISCORD_RELAY_ENABLED=false            turn the whole feature off (default on; false/0/off disable it)
//	DISCORD_GATEWAY_LOCK_KEY=<int64>       advisory-lock key for gateway leadership (default 80920032)
//	DISCORD_GATEWAY_SINGLE_INSTANCE=true   lead without a lock (one process only)
//
// Bot tokens are encrypted at rest with the MCP token key (TOKEN_ENCRYPTION_SECRET);
// without it no bot can be added. See docs/adr/0x025-discord-relay.md.
package discordplugin

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay/gateway"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

// active is the running relay, set when the plugin is applied. The reply hook is
// registered at init (replyhook requires it) and does nothing until then.
var active atomic.Pointer[discordrelay.Service]

// unregisterHook removes the reply hook again when the relay turns out to be
// switched off.
var unregisterHook func()

func init() {
	unregisterHook = replyhook.Register(func(ctx context.Context, ev replyhook.Event) {
		if svc := active.Load(); svc != nil {
			svc.OnReply(ctx, ev)
		}
	})
	plugins.Register(apply)
}

// enabled reads DISCORD_RELAY_ENABLED. It is read when the plugin is applied, not
// at package init, so a value from the server's .env file (loaded by main's init,
// which runs after this package's) is honoured.
func enabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("DISCORD_RELAY_ENABLED")))
	return v != "false" && v != "0" && v != "off"
}

func apply(d plugins.Deps) {
	if !enabled() {
		if unregisterHook != nil {
			unregisterHook()
		}
		if d.Logger != nil {
			d.Logger.Info("discord relay: disabled by DISCORD_RELAY_ENABLED")
		}
		return
	}
	if d.DataStore == nil || d.Logger == nil || d.AuthRouter == nil {
		return
	}
	logger := d.Logger.Named("discord")
	rest := discordrelay.NewREST()

	routes := d.AuthRouter.PathPrefix("/discord").Subrouter()
	h := &Handler{Store: d.DataStore, Discord: rest, Files: d.FileStore, Logger: logger}
	h.Register(routes)

	if d.Turns == nil || d.Lifecycle == nil {
		logger.Warn("discord relay: no turn starter or lifecycle; replies to tags are disabled")
		return
	}
	svc := &discordrelay.Service{Store: d.DataStore, Discord: rest, Turns: d.Turns, Logger: logger}
	if d.FileStore != nil {
		svc.Files = d.FileStore
	}
	if d.Attachments != nil {
		svc.Ingest = d.Attachments
		svc.Fetch = discordrelay.NewCDNFetcher()
	}
	active.Store(svc)

	sup := &gateway.Supervisor{
		Locker: locker(d, logger),
		Source: botSource{ds: d.DataStore},
		Sink:   sink{svc: svc},
		Dial:   gateway.DiscordgoDialer(logger),
		Logger: logger,
	}
	go sup.Run(d.Lifecycle)
	logger.Info("discord relay: routes registered, gateway supervisor started")
}

func locker(d plugins.Deps, logger *zap.Logger) gateway.Locker {
	if v := strings.TrimSpace(os.Getenv("DISCORD_GATEWAY_SINGLE_INSTANCE")); v == "true" || v == "1" {
		logger.Info("discord relay: single-instance gateway (no leader lock)")
		return gateway.SingleInstanceLocker{}
	}
	key := gateway.DefaultLockKey
	if v := strings.TrimSpace(os.Getenv("DISCORD_GATEWAY_LOCK_KEY")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			key = parsed
		} else {
			logger.Warn("discord relay: ignoring invalid DISCORD_GATEWAY_LOCK_KEY", zap.String("value", v))
		}
	}
	return gateway.AdvisoryLocker{DS: d.DataStore, Key: key}
}

// botStore is the slice of the datastore the gateway's bot list needs.
type botStore interface {
	ListActiveDiscordBotCredentials(ctx context.Context) ([]models.DiscordBotCredentials, error)
}

type botSource struct{ ds botStore }

func (b botSource) ActiveBots(ctx context.Context) ([]gateway.Bot, error) {
	creds, err := b.ds.ListActiveDiscordBotCredentials(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.Bot, 0, len(creds))
	for _, c := range creds {
		out = append(out, gateway.Bot{ID: c.ID, Token: c.Token, BotUserID: c.BotUserID, MessageContent: c.MessageContent})
	}
	return out, nil
}

// inbound is what the gateway sink hands messages to (the relay service).
type inbound interface {
	HandleInbound(ctx context.Context, botID uuid.UUID, botUserID string, msg discordrelay.InboundMessage)
}

type sink struct{ svc inbound }

func (s sink) HandleInbound(ctx context.Context, bot gateway.Bot, msg discordrelay.InboundMessage) {
	s.svc.HandleInbound(ctx, bot.ID, bot.BotUserID, msg)
}
