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

var errTurnMissingTarget = errors.New("plugin turn: user id and chat id (or message id) are required")

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

// SupersedeTurn implements plugins.TurnReviser through agent.SupersedeTurn.
func (s agentTurnStarter) SupersedeTurn(ctx context.Context, userID, userMessageID uuid.UUID) error {
	if userID == uuid.Nil || userMessageID == uuid.Nil {
		return errTurnMissingTarget
	}
	return s.agent.SupersedeTurn(ctx, userID, userMessageID)
}

// RegenerateTurn implements plugins.TurnReviser through agent.RegenerateTurn, with the same user
// context and turn source StartUserTurn builds.
func (s agentTurnStarter) RegenerateTurn(ctx context.Context, turn plugins.TurnRegeneration) (*models.ChatMessageResponse, error) {
	if turn.UserID == uuid.Nil || turn.UserMessageID == uuid.Nil {
		return nil, errTurnMissingTarget
	}
	ctx = middleware.ContextWithUser(ctx, turn.UserID, turn.Timezone)
	source := turn.Source
	if source == "" {
		source = metering.TurnSourcePlugin
	}
	ctx = metering.WithTurnSource(ctx, source)
	return s.agent.RegenerateTurn(ctx, turn.UserMessageID, turn.Options)
}
