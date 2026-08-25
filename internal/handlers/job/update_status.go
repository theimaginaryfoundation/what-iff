package job

import (
	"encoding/json"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// StatusUpdateRequest represents the request body for updating job status
type StatusUpdateRequest struct {
	Status   models.JobStatus `json:"status"`
	ErrorMsg string           `json:"error_message,omitempty"`
}

// UpdateJobStatus handles updating a job's status
func (h *Handler) UpdateJobStatus(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("Failed to get user ID from context")
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get job ID from URL using Gorilla Mux vars
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Job ID is required", nil)
		return
	}

	// Parse the ID
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		h.logger.Error("Invalid job ID", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid job ID", err)
		return
	}

	// Parse request body
	var request StatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.logger.Error("Invalid request body", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate status
	if request.Status == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Status is required", nil)
		return
	}

	// Update job status using the provider
	job, err := h.provider.UpdateJobStatus(r.Context(), userID, jobID, request.Status, request.ErrorMsg)
	if err != nil {
		if err == datastore.ErrJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Job not found", nil)
		} else if err == datastore.ErrInvalidJobStatus {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid job status", nil)
		} else {
			h.logger.Error("Error updating job status", zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error updating job status", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}
