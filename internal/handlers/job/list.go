package job

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"go.uber.org/zap"
)

// ListJobs handles listing jobs with filtering and pagination
func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("Failed to get user ID from context")
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Parse query parameters for pagination
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page := handlerutils.ParseIntParam(pageStr, 1)
	pageSize := handlerutils.ParseIntParam(pageSizeStr, 10)

	// Create filters from query parameters
	var filters models.JobFilters

	// Handle job type filter
	jobType := r.URL.Query().Get("job_type")
	if jobType != "" {
		filters.JobType = &jobType
	}

	// Handle reference filter
	reference := r.URL.Query().Get("reference")
	if reference != "" {
		filters.Reference = &reference
	}

	// Handle status filter
	statusStr := r.URL.Query().Get("status")
	if statusStr != "" {
		status := models.JobStatus(statusStr)
		filters.Status = &status
	}

	// List jobs using the provider
	paginatedResponse, err := h.provider.ListJobs(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("Error listing jobs", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error listing jobs", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, paginatedResponse)
}
