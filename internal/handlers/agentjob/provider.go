package agentjob

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providers"

	"github.com/google/uuid"
)

type AgentJobProvider interface {
	providers.AgentJobProvider
}

type ScheduleParser interface {
	ParseAgentJobSchedule(ctx context.Context, userID uuid.UUID, scheduleInput string, timezone string, now time.Time) (*models.AgentJobSchedulePreview, error)
}

type AgentJobRunner interface {
	RunAgentJobNow(ctx context.Context, userID, id uuid.UUID) error
}
