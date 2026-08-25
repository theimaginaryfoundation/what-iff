package providers

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
)

// AgentJobProvider defines the interface for AgentJob data operations.
type AgentJobProvider interface {
	CreateAgentJob(ctx context.Context, userID uuid.UUID, jobModel models.AgentJob) (*models.AgentJob, error)
	ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error)
	GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error)
	UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error)
	UpdateAgentJobPrompt(ctx context.Context, userID, id uuid.UUID, prompt string) (*models.AgentJob, error)
	UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error)
	UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error)
	SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error)
	SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error)
	DeleteAgentJob(ctx context.Context, userID, id uuid.UUID) error
	AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error
	RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error
}
