package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const personalityGenerationJobTimeout = 2 * time.Minute

// EnqueuePersonalityGenerationJob starts async generation for a wizard flow.
func (a *Agent) EnqueuePersonalityGenerationJob(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error) {
	if a == nil || a.ds == nil {
		return nil, fmt.Errorf("personality generation job: agent not configured")
	}

	// Personality generation shares the same per-user slot as portrait/expression jobs.
	activeBackgroundJob, err := a.ds.FindActivePersonalityMediaJob(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find active personality background job: %w", err)
	}
	if activeBackgroundJob != nil {
		return nil, &ErrPersonalityMediaJobActive{Job: activeBackgroundJob}
	}

	active, err := a.ds.FindActivePersonalityGenerationJob(ctx, userID, flowID)
	if err != nil {
		return nil, fmt.Errorf("find active personality generation job: %w", err)
	}
	if active != nil {
		return nil, &ErrPersonalityMediaJobActive{Job: active}
	}

	newJob, err := a.ds.CreateJob(ctx, userID, models.Job{
		JobType:   JobTypePersonalityGeneration,
		Reference: flowID.String(),
		Status:    models.JobStatusPending,
	})
	if err != nil {
		return nil, fmt.Errorf("create personality generation job: %w", err)
	}
	a.logger.Info("personality generation job enqueued",
		zap.String("job_id", newJob.ID.String()),
		zap.String("user_id", userID.String()),
		zap.String("flow_id", flowID.String()))

	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	go a.runPersonalityMediaJob(detachedCtx, newJob, func(runCtx context.Context) (uuid.UUID, error) {
		jobStart := time.Now()
		a.logger.Info("personality generation job started",
			zap.String("job_id", newJob.ID.String()),
			zap.String("user_id", userID.String()),
			zap.String("flow_id", flowID.String()))

		// Keep generation bounded so jobs cannot remain in processing indefinitely.
		timedCtx, cancel := context.WithTimeout(runCtx, personalityGenerationJobTimeout)
		defer cancel()

		loadStart := time.Now()
		a.logger.Info("personality generation loading flow",
			zap.String("job_id", newJob.ID.String()),
			zap.String("flow_id", flowID.String()))
		flow, getErr := a.ds.GetFlow(timedCtx, userID, flowID)
		if getErr != nil {
			return uuid.Nil, fmt.Errorf("load flow: %w", getErr)
		}
		a.logger.Info("personality generation flow loaded",
			zap.String("job_id", newJob.ID.String()),
			zap.String("flow_id", flowID.String()),
			zap.Int("answer_count", len(flow.Answers)),
			zap.Duration("elapsed", time.Since(loadStart)))
		if len(flow.Answers) == 0 {
			return uuid.Nil, fmt.Errorf("no answers provided yet")
		}

		genStart := time.Now()
		a.logger.Info("personality generation calling model",
			zap.String("job_id", newJob.ID.String()),
			zap.String("flow_id", flowID.String()))
		result, genErr := a.GeneratePersonality(timedCtx, flow.Answers)
		if genErr != nil {
			return uuid.Nil, fmt.Errorf("generate personality: %w", genErr)
		}
		a.logger.Info("personality generation model call completed",
			zap.String("job_id", newJob.ID.String()),
			zap.String("flow_id", flowID.String()),
			zap.Int("name_count", len(result.Names)),
			zap.Int("prompt_len", len(result.SystemPrompt)),
			zap.Duration("elapsed", time.Since(genStart)))

		saveStart := time.Now()
		a.logger.Info("personality generation saving flow output",
			zap.String("job_id", newJob.ID.String()),
			zap.String("flow_id", flowID.String()))
		if _, saveErr := a.ds.SetFlowGenerated(timedCtx, userID, flowID, result.SystemPrompt, result.AboutMe, result.Names); saveErr != nil {
			return uuid.Nil, fmt.Errorf("save generated flow: %w", saveErr)
		}
		a.logger.Info("personality generation flow output saved",
			zap.String("job_id", newJob.ID.String()),
			zap.String("flow_id", flowID.String()),
			zap.Duration("elapsed", time.Since(saveStart)),
			zap.Duration("total_elapsed", time.Since(jobStart)))

		return flowID, nil
	})

	return newJob, nil
}
