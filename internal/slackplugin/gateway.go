package slackplugin

import (
	"context"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/slackrelay"
	"go.uber.org/zap"
)

// gateway opens the Socket Mode sessions the supervisor asks for.
type gateway struct {
	opener slackrelay.Opener
	dialer *websocket.Dialer
	store  stateStore
	events eventSink
	logger *zap.Logger
}

// stateStore is where the gateway records connection health.
type stateStore interface {
	SetSlackAppState(ctx context.Context, id uuid.UUID, status models.SlackAppStatus, lastError string) error
}

// eventSink is what the gateway hands events to (the relay service).
type eventSink interface {
	HandleEvent(ctx context.Context, app models.SlackAppCredentials, cb slackrelay.EventCallback)
}

func (g *gateway) dial(ctx context.Context, app models.SlackAppCredentials) (relaykit.Conn, error) {
	log := g.logger.With(zap.String("slack_app_id", app.ID.String()), zap.String("app", app.AppID))
	return slackrelay.OpenSocket(ctx, slackrelay.SocketConfig{
		AppToken: app.AppToken,
		Opener:   g.opener,
		Dialer:   g.dialer,
		Logger:   log,
		OnEvent: func(cb slackrelay.EventCallback) {
			// Off the read loop: the relay reads the database and may call Slack.
			go g.events.HandleEvent(ctx, app, cb)
		},
		OnState: func(st slackrelay.SocketState, detail string) {
			status, lastError := models.SlackAppActive, detail
			if st == slackrelay.SocketInvalidToken {
				status = models.SlackAppInvalidToken
			}
			if err := g.store.SetSlackAppState(context.WithoutCancel(ctx), app.ID, status, lastError); err != nil {
				log.Warn("slack gateway: record connection state", zap.Error(err))
			}
		},
	}), nil
}
