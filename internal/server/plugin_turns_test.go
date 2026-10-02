package server

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
)

func TestAgentTurnStarterRejectsATurnWithoutAUserOrChat(t *testing.T) {
	// A nil agent proves the guard runs before any agent call.
	s := agentTurnStarter{}
	for name, turn := range map[string]plugins.UserTurn{
		"no user": {Message: models.ChatMessage{ChatID: uuid.New(), Message: "hi"}},
		"no chat": {UserID: uuid.New(), Message: models.ChatMessage{Message: "hi"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.StartUserTurn(context.Background(), turn)
			assert.ErrorIs(t, err, errTurnMissingTarget)
		})
	}
}
