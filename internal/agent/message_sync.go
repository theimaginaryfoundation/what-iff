package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// HandleUserMessageSync persists a message and runs the agent synchronously.
// It is intended for background/scheduled execution paths that need a deterministic completion point.
func (a *Agent) HandleUserMessageSync(ctx context.Context, request models.ChatMessage) (*models.ChatMessage, error) {
	ctx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	userID, _ := middleware.GetUserIDFromContext(ctx)

	// System rituals are UI-only and must never be persisted.
	dbRituals, systemRituals := SplitRituals(request.Rituals)
	request.Rituals = dbRituals

	chatMessage, err := a.ds.CreateChatMessage(ctx, userID, request)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat message: %w", err)
	}
	if len(systemRituals) > 0 {
		chatMessage.Rituals = append(chatMessage.Rituals, systemRituals...)
	}

	runJob, err := a.ds.CreateJob(ctx, userID, models.Job{
		JobType:     JobTypeAgentJobRun,
		Reference:   chatMessage.ID.String(),
		Status:      models.JobStatusPending,
		DraftDeltas: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	agentMessage, err := a.handleUserMessage(ctx, runJob, chatMessage)
	if err != nil {
		// `handleUserMessage` is responsible for setting job status.
		return agentMessage, err
	}

	// `handleUserMessage` typically completes the job, but ensure we never leave it pending/processing
	// or stuck in an intermediate phase on success.
	if runJob.Status != models.JobStatusComplete && runJob.Status != models.JobStatusFailed {
		runJob.Status = models.JobStatusComplete
		if agentMessage != nil {
			runJob.ResultID = &agentMessage.ID
		}
		if _, uerr := a.ds.UpdateJob(ctx, runJob.UserID, *runJob); uerr != nil {
			a.logger.Error("failed to update sync job status to complete",
				zap.String("job_id", runJob.ID.String()),
				zap.Error(uerr),
			)
		}
	}

	return agentMessage, nil
}
