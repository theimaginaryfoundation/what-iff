package providers

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
)

// JobProvider defines the interface for job data operations
type JobProvider interface {
	// CreateJob creates a new job
	CreateJob(ctx context.Context, userID uuid.UUID, jobModel models.Job) (*models.Job, error)

	// ListJobs returns a paginated list of jobs filtered by the provided criteria
	ListJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.JobFilters) (*models.PaginatedResponse, error)

	// GetJob retrieves a single job by ID
	GetJob(ctx context.Context, userID, id uuid.UUID) (*models.Job, error)

	// UpdateJobStatus updates the status of a job
	UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error)

	// SetJobResult sets the result ID for a completed job
	SetJobResult(ctx context.Context, userID, id uuid.UUID, resultID uuid.UUID) (*models.Job, error)
}
