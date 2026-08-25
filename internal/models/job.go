package models

import (
	"time"

	"github.com/google/uuid"
)

// JobStatus represents the status of an asynchronous job
type JobStatus string

// Job status constants
const (
	JobStatusPending            JobStatus = "pending"
	JobStatusProcessing         JobStatus = "processing"
	JobStatusInferenceComplete  JobStatus = "inference_complete"
	JobStatusExpressionComplete JobStatus = "expression_complete"
	JobStatusCompactionComplete JobStatus = "compaction_complete"
	JobStatusComplete           JobStatus = "complete"
	JobStatusCancelled          JobStatus = "cancelled"
	JobStatusFailed             JobStatus = "failed"
)

// Job represents an asynchronous background job in the system
type Job struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	JobType   string     `json:"job_type"`
	Reference string     `json:"reference"`
	Status    JobStatus  `json:"status"`
	Error     string     `json:"error,omitempty"`
	ResultID  *uuid.UUID `json:"result_id,omitempty"`
	// DraftDeltas are incremental assistant text chunks emitted while inference is in progress.
	DraftDeltas []string `json:"draft_deltas,omitempty"`
	// Progress is an optional JSON-encoded payload for long-running jobs (e.g. chat import counts).
	// Clients should treat it as opaque and parse per job_type.
	Progress  string    `json:"progress,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JobFilters defines filters for listing jobs
type JobFilters struct {
	JobType   *string    `json:"job_type,omitempty"`
	Reference *string    `json:"reference,omitempty"`
	Status    *JobStatus `json:"status,omitempty"`
}
