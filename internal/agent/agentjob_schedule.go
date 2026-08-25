package agent

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/schedule"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
)

// ParseAgentJobSchedule converts a natural language schedule into a validated schedule spec and preview.
func (a *Agent) ParseAgentJobSchedule(ctx context.Context, userID uuid.UUID, scheduleInput string, timezone string, now time.Time) (*models.AgentJobSchedulePreview, error) {
	return schedule.Parse(ctx, a.OpenAIProvider, userID, scheduleInput, timezone, now)
}
