package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// EnqueuePersonalityPortraitJob generates a wizard portrait for the given flow in the background.
// imageStyle is the style hint from the wizard (e.g. "auto", "anime"). Must not be "none" — callers guard.
func (a *Agent) EnqueuePersonalityPortraitJob(ctx context.Context, userID, flowID uuid.UUID, systemPrompt, imageStyle string) (*models.Job, error) {
	if a == nil || a.ds == nil {
		return nil, fmt.Errorf("personality portrait job: agent not configured")
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return nil, fmt.Errorf("personality portrait job: system prompt is required")
	}

	active, err := a.ds.FindActivePersonalityMediaJob(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find active personality media job: %w", err)
	}
	if active != nil {
		return nil, &ErrPersonalityMediaJobActive{Job: active}
	}

	newJob, err := a.ds.CreateJob(ctx, userID, models.Job{
		JobType:   JobTypePersonalityPortrait,
		Reference: flowID.String(),
		Status:    models.JobStatusPending,
	})
	if err != nil {
		return nil, fmt.Errorf("create personality portrait job: %w", err)
	}

	prompt := systemPrompt
	style := imageStyle
	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	go a.runPersonalityMediaJob(detachedCtx, newJob, func(runCtx context.Context) (uuid.UUID, error) {
		att, err := a.GeneratePersonalityPortraitImage(runCtx, userID, prompt, style)
		if err != nil {
			return uuid.Nil, err
		}
		return att.ID, nil
	})

	return newJob, nil
}
