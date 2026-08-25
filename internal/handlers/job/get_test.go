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
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

func TestGetJob(t *testing.T) {
	// Create a mock provider and handler
	mockProvider := NewMockJobProvider()
	logger, _ := zap.NewDevelopment()
	handler := NewHandler(mockProvider, logger)

	// Create test user ID
	userID := uuid.New()

	// Add a test job
	job := models.Job{
		UserID:    userID,
		JobType:   "content_generation",
		Reference: "project-123",
		Status:    models.JobStatusPending,
		Progress:  `{"phase":"complete","total":115,"imported":115,"skipped":0}`,
	}
	createdJob := mockProvider.AddJob(job)

	t.Run("Success case", func(t *testing.T) {
		// Create a request
		req, err := http.NewRequest(http.MethodGet, "/api/job/"+createdJob.ID.String(), nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set job ID in URL vars
		vars := map[string]string{
			"id": createdJob.ID.String(),
		}
		req = mux.SetURLVars(req, vars)

		// Add user ID to context
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
		req = req.WithContext(ctx)

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler.GetJob(rr, req)

		// Check the status code
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}
		if cacheControl := rr.Header().Get("Cache-Control"); cacheControl != "no-store" {
			t.Errorf("Handler returned unexpected Cache-Control header: got %q want %q", cacheControl, "no-store")
		}

		// Check the response body
		var responseJob models.Job
		err = json.Unmarshal(rr.Body.Bytes(), &responseJob)
		if err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if responseJob.ID != createdJob.ID {
			t.Errorf("Handler returned unexpected job ID: got %v want %v", responseJob.ID, createdJob.ID)
		}

		if responseJob.JobType != job.JobType {
			t.Errorf("Handler returned unexpected job type: got %v want %v", responseJob.JobType, job.JobType)
		}

		if responseJob.Status != job.Status {
			t.Errorf("Handler returned unexpected job status: got %v want %v", responseJob.Status, job.Status)
		}
		if responseJob.Progress != job.Progress {
			t.Errorf("Handler returned unexpected job progress: got %q want %q", responseJob.Progress, job.Progress)
		}
	})

	t.Run("Not found case", func(t *testing.T) {
		// Test not found case with non-existent ID
		nonExistentID := uuid.New()

		// Create a new request with the non-existent ID
		req, _ := http.NewRequest(http.MethodGet, "/api/job/"+nonExistentID.String(), nil)

		// Set job ID in URL vars
		vars := map[string]string{
			"id": nonExistentID.String(),
		}
		req = mux.SetURLVars(req, vars)

		// Add user ID to context
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
		req = req.WithContext(ctx)

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler.GetJob(rr, req)

		// Check the status code
		if status := rr.Code; status != http.StatusNotFound {
			t.Errorf("Handler returned wrong status code for not found: got %v want %v", status, http.StatusNotFound)
		}
	})

	t.Run("Unauthorized case", func(t *testing.T) {
		// Test unauthorized case with different user ID
		otherUserID := uuid.New()

		// Create a job for a different user
		otherUserJob := models.Job{
			UserID:    otherUserID,
			JobType:   "content_generation",
			Reference: "project-456",
			Status:    models.JobStatusPending,
		}
		otherJob := mockProvider.AddJob(otherUserJob)

		// Create a new request with the other user's job ID
		req, _ := http.NewRequest(http.MethodGet, "/api/job/"+otherJob.ID.String(), nil)

		// Set job ID in URL vars
		vars := map[string]string{
			"id": otherJob.ID.String(),
		}
		req = mux.SetURLVars(req, vars)

		// Add original user ID to context (so it's unauthorized)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
		req = req.WithContext(ctx)

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler.GetJob(rr, req)

		// Check the status code
		if status := rr.Code; status != http.StatusUnauthorized {
			t.Errorf("Handler returned wrong status code for unauthorized: got %v want %v", status, http.StatusUnauthorized)
		}
	})
}
