package job

import (
	"encoding/json"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// ResultUpdateRequest represents the request body for setting a job result
type ResultUpdateRequest struct {
	ResultID uuid.UUID `json:"result_id"`
}

// SetJobResult handles setting a job's result ID
func (h *Handler) SetJobResult(w http.ResponseWriter, r *http.Request) {
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
	var request ResultUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.logger.Error("Invalid request body", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate result ID
	if request.ResultID == uuid.Nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Result ID is required", nil)
		return
	}

	// Set job result using the provider
	job, err := h.provider.SetJobResult(r.Context(), userID, jobID, request.ResultID)
	if err != nil {
		if err == datastore.ErrJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Job not found", nil)
		} else {
			h.logger.Error("Error setting job result", zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error setting job result", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}
