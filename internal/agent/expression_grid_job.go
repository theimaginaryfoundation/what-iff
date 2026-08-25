package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// EnqueueExpressionGridJob starts a background expression_grid job when no other personality media job is active.
func (a *Agent) EnqueueExpressionGridJob(ctx context.Context, userID, personalityID uuid.UUID) (*models.Job, error) {
	if a == nil || a.ds == nil {
		return nil, fmt.Errorf("expression grid job: agent not configured")
	}

	active, err := a.ds.FindActivePersonalityMediaJob(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find active personality media job: %w", err)
	}
	if active != nil {
		return nil, &ErrPersonalityMediaJobActive{Job: active}
	}

	newJob, err := a.ds.CreateJob(ctx, userID, models.Job{
		JobType:   JobTypeExpressionGrid,
		Reference: personalityID.String(),
		Status:    models.JobStatusPending,
	})
	if err != nil {
		return nil, fmt.Errorf("create expression grid job: %w", err)
	}

	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	go a.runPersonalityMediaJob(detachedCtx, newJob, func(runCtx context.Context) (uuid.UUID, error) {
		if _, err := a.GenerateDefaultExpressionGrid(runCtx, userID, personalityID); err != nil {
			return uuid.Nil, err
		}
		return personalityID, nil
	})

	return newJob, nil
}
