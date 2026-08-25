package job

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestListJobs(t *testing.T) {
	// Create a mock provider and handler
	mockProvider := NewMockJobProvider()
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(mockProvider, logger)

	// Create test user ID
	userID := uuid.New()
	otherUserID := uuid.New()

	// Add some test jobs
	contentGenJob := models.Job{
		UserID:    userID,
		JobType:   "content_generation",
		Reference: "project-123",
		Status:    models.JobStatusPending,
	}

	seoAnalysisJob := models.Job{
		UserID:    userID,
		JobType:   "seo_analysis",
		Reference: "project-123",
		Status:    models.JobStatusProcessing,
	}

	completedJob := models.Job{
		UserID:    userID,
		JobType:   "content_generation",
		Reference: "project-456",
		Status:    models.JobStatusComplete,
	}

	otherUserJob := models.Job{
		UserID:    otherUserID,
		JobType:   "content_generation",
		Reference: "project-789",
		Status:    models.JobStatusPending,
	}

	mockProvider.AddJob(contentGenJob)
	mockProvider.AddJob(seoAnalysisJob)
	mockProvider.AddJob(completedJob)
	mockProvider.AddJob(otherUserJob)

	// Create a request
	req, err := createRequest(http.MethodGet, "/api/job", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Call the handler
	handler.ListJobs(rr, req)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check the response body
	var response models.PaginatedResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should return only the user's jobs, not the other user's job
	if response.TotalCount != 3 {
		t.Errorf("Handler returned unexpected number of jobs: got %v want %v", response.TotalCount, 3)
	}

	// Test filtering by job type
	req, _ = createRequest(http.MethodGet, "/api/job?job_type=content_generation", nil)
	req = req.WithContext(ctx)

	rr = httptest.NewRecorder()
	handler.ListJobs(rr, req)

	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should return only content_generation jobs for the user
	if response.TotalCount != 2 {
		t.Errorf("Handler returned unexpected number of jobs when filtering by job type: got %v want %v", response.TotalCount, 2)
	}

	// Test filtering by status
	req, _ = createRequest(http.MethodGet, "/api/job?status=complete", nil)
	req = req.WithContext(ctx)

	rr = httptest.NewRecorder()
	handler.ListJobs(rr, req)

	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should return only completed jobs for the user
	if response.TotalCount != 1 {
		t.Errorf("Handler returned unexpected number of jobs when filtering by status: got %v want %v", response.TotalCount, 1)
	}

	// Test multiple filters
	req, _ = createRequest(http.MethodGet, "/api/job?job_type=content_generation&reference=project-123", nil)
	req = req.WithContext(ctx)

	rr = httptest.NewRecorder()
	handler.ListJobs(rr, req)

	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should return only content_generation jobs with reference project-123 for the user
	if response.TotalCount != 1 {
		t.Errorf("Handler returned unexpected number of jobs when using multiple filters: got %v want %v", response.TotalCount, 1)
	}
}
