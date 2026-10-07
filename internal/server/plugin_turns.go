package server

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
)

// agentTurnStarter is the plugins.TurnStarter the server hands to plugins. It
// builds the same user context the webhook auth middleware does, then starts the
// turn through agent.HandleUserMessage, the path the app and the webhook user
// mode take, so a plugin-started turn is saved, metered and finished like any
// other.
type agentTurnStarter struct {
	agent *agent.Agent
}

var errTurnMissingTarget = errors.New("plugin turn: user id and chat id are required")

func (s agentTurnStarter) StartUserTurn(ctx context.Context, turn plugins.UserTurn) (*models.ChatMessageResponse, error) {
	if turn.UserID == uuid.Nil || turn.Message.ChatID == uuid.Nil {
		return nil, errTurnMissingTarget
	}
	ctx = middleware.ContextWithUser(ctx, turn.UserID, turn.Timezone)
	source := turn.Source
	if source == "" {
		source = metering.TurnSourcePlugin
	}
	ctx = metering.WithTurnSource(ctx, source)
	msg := turn.Message
	msg.Origin = models.MessageOriginUser
	return s.agent.HandleUserMessage(ctx, msg)
}
