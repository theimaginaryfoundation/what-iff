package job

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// MockJobProvider is a mock implementation of the JobProvider interface
type MockJobProvider struct {
	jobs map[uuid.UUID]*models.Job

	// Mock behavior flags and error responses
	shouldFailGetJob       bool
	getJobError            error
	shouldFailListJobs     bool
	listJobsError          error
	shouldFailUpdateStatus bool
	updateStatusError      error
	shouldFailSetResult    bool
	setResultError         error
	shouldFailCreateJob    bool
	createJobError         error
}

// NewMockJobProvider creates a new mock job provider
func NewMockJobProvider() *MockJobProvider {
	return &MockJobProvider{
		jobs: make(map[uuid.UUID]*models.Job),
	}
}

// CreateJob mocks creating a new job
func (m *MockJobProvider) CreateJob(ctx context.Context, userID uuid.UUID, job models.Job) (*models.Job, error) {
	// Return configured error if set to fail
	if m.shouldFailCreateJob {
		return nil, m.createJobError
	}

	// Set the user ID
	job.UserID = userID

	// Generate ID if not provided
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}

	// Set timestamps if not provided
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
		job.UpdatedAt = time.Now()
	}

	// Save the job
	newJob := job
	m.jobs[job.ID] = &newJob

	return &newJob, nil
}

// ListJobs mocks listing jobs with pagination and filtering
func (m *MockJobProvider) ListJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.JobFilters) (*models.PaginatedResponse, error) {
	// Return configured error if set to fail
	if m.shouldFailListJobs {
		return nil, m.listJobsError
	}

	var results []any

	for _, j := range m.jobs {
		// Skip if not owned by the user
		if j.UserID != userID {
			continue
		}

		// Apply filters if provided
		if filters.JobType != nil && *filters.JobType != "" && j.JobType != *filters.JobType {
			continue
		}

		if filters.Reference != nil && *filters.Reference != "" && j.Reference != *filters.Reference {
			continue
		}

		if filters.Status != nil && j.Status != *filters.Status {
			continue
		}

		results = append(results, j)
	}

	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: len(results),
		Page:       pageNum,
	}, nil
}

// GetJob mocks retrieving a job by ID
func (m *MockJobProvider) GetJob(ctx context.Context, userID, id uuid.UUID) (*models.Job, error) {
	// Return configured error if set to fail
	if m.shouldFailGetJob {
		return nil, m.getJobError
	}

	job, exists := m.jobs[id]
	if !exists {
		return nil, datastore.ErrJobNotFound
	}

	// Check ownership
	if job.UserID != userID {
		return nil, datastore.ErrUnauthorized
	}

	return job, nil
}

// UpdateJobStatus mocks updating a job's status
func (m *MockJobProvider) UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error) {
	// Return configured error if set to fail
	if m.shouldFailUpdateStatus {
		return nil, m.updateStatusError
	}

	job, exists := m.jobs[id]
	if !exists {
		return nil, datastore.ErrJobNotFound
	}

	// Check ownership
	if job.UserID != userID {
		return nil, datastore.ErrUnauthorized
	}

	// Validate status
	validStatus := false
	for _, s := range []models.JobStatus{
		models.JobStatusPending,
		models.JobStatusProcessing,
		models.JobStatusInferenceComplete,
		models.JobStatusExpressionComplete,
		models.JobStatusCompactionComplete,
		models.JobStatusComplete,
		models.JobStatusCancelled,
		models.JobStatusFailed,
	} {
		if status == s {
			validStatus = true
			break
		}
	}
	if !validStatus {
		return nil, datastore.ErrInvalidJobStatus
	}

	// Update job
	job.Status = status
	job.Error = errorMsg
	job.UpdatedAt = time.Now()

	return job, nil
}

// SetJobResult mocks setting a job's result ID
func (m *MockJobProvider) SetJobResult(ctx context.Context, userID, id uuid.UUID, resultID uuid.UUID) (*models.Job, error) {
	// Return configured error if set to fail
	if m.shouldFailSetResult {
		return nil, m.setResultError
	}

	job, exists := m.jobs[id]
	if !exists {
		return nil, datastore.ErrJobNotFound
	}

	// Check ownership
	if job.UserID != userID {
		return nil, datastore.ErrUnauthorized
	}

	// Update job
	job.ResultID = &resultID
	job.Status = models.JobStatusComplete
	job.UpdatedAt = time.Now()

	return job, nil
}

// AddJob adds a job to the mock provider (helper for tests)
func (m *MockJobProvider) AddJob(job models.Job) *models.Job {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
		job.UpdatedAt = time.Now()
	}

	// Create a new copy to avoid pointer issues
	newJob := job
	m.jobs[job.ID] = &newJob

	return &newJob
}

// Helper function to create a request
func createRequest(method, url string, body interface{}) (*http.Request, error) {
	var bodyReader *bytes.Buffer

	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(bodyBytes)
	} else {
		bodyReader = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// setJobIDInURLVars sets the job ID in the URL vars for the request
func setJobIDInURLVars(req *http.Request, jobID uuid.UUID) *http.Request {
	vars := map[string]string{
		"id": jobID.String(),
	}

	return mux.SetURLVars(req, vars)
}
