package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"go.uber.org/zap"
)

// entitySpotMaxCards caps how many entity cards one turn brings into context.
const entitySpotMaxCards = 5

// spotEntityCards finds the entities the user's message mentions by name or alias and renders
// their cards for this turn's context. Best effort: a failure means no cards, never a failed turn.
func (a *Agent) spotEntityCards(ctx context.Context, userID, personalityID uuid.UUID, message string) string {
	if a.entityTool == nil {
		return ""
	}
	found, err := a.entityTool.Spot(ctx, userID, personalityID, message, entitySpotMaxCards)
	if err != nil {
		a.logger.Debug("entity spotting failed; continuing without cards", zap.Error(err))
		return ""
	}
	return tools.RenderEntityCards(found)
}
