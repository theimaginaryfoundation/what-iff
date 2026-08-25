package agent

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// runPersonalityMediaJob executes work in the background with standard status transitions.
func (a *Agent) runPersonalityMediaJob(
	parentCtx context.Context,
	job *models.Job,
	work func(context.Context) (resultID uuid.UUID, err error),
) {
	userID := job.UserID
	jobID := job.ID

	runCtx, ok := middleware.CopyUserToIDContext(parentCtx, context.Background())
	if !ok {
		const msg = "user ID not found in context"
		if _, err := a.ds.UpdateJobStatus(context.Background(), userID, jobID, models.JobStatusFailed, msg); err != nil {
			a.logger.Error("personality media job: failed to mark failed after missing user context",
				zap.String("job_id", jobID.String()),
				zap.Error(err))
		}
		a.logger.Error("personality media job: missing user in context",
			zap.String("job_id", jobID.String()))
		return
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			panicMessage := fmt.Sprintf("panic: %v", recovered)
			if _, err := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, panicMessage); err != nil {
				a.logger.Error("personality media job: failed to mark failed after panic",
					zap.String("job_id", jobID.String()),
					zap.Error(err))
			}
			a.logger.Error("panic recovered in personality media job",
				zap.String("job_id", jobID.String()),
				zap.String("job_type", job.JobType),
				zap.Any("panic", recovered),
				zap.ByteString("stack_trace", debug.Stack()),
			)
		}
	}()

	if _, err := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusProcessing, ""); err != nil {
		msg := fmt.Sprintf("mark processing: %v", err)
		if _, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, msg); failErr != nil {
			a.logger.Error("personality media job: failed to mark failed after processing transition error",
				zap.String("job_id", jobID.String()),
				zap.Error(failErr))
		}
		a.logger.Error("personality media job: failed to mark processing",
			zap.String("job_id", jobID.String()),
			zap.Error(err))
		return
	}

	resultID, err := work(runCtx)
	if err != nil {
		if _, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, err.Error()); failErr != nil {
			a.logger.Error("personality media job: failed to mark failed",
				zap.String("job_id", jobID.String()),
				zap.Error(failErr))
		}
		a.logger.Error("personality media job failed",
			zap.String("job_id", jobID.String()),
			zap.String("job_type", job.JobType),
			zap.Error(err))
		return
	}

	if _, err := a.ds.SetJobResult(runCtx, userID, jobID, resultID); err != nil {
		msg := fmt.Sprintf("set job result: %v", err)
		if _, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, msg); failErr != nil {
			a.logger.Error("personality media job: failed to mark failed after set result error",
				zap.String("job_id", jobID.String()),
				zap.Error(failErr))
		}
		a.logger.Error("personality media job: failed to set result",
			zap.String("job_id", jobID.String()),
			zap.Error(err))
	}
}
