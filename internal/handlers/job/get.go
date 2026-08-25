package job

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// GetJob handles retrieving a job by ID
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
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

	// Get job using the provider
	job, err := h.provider.GetJob(r.Context(), userID, jobID)
	if err != nil {
		if err == datastore.ErrJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Job not found", nil)
		} else if err == datastore.ErrUnauthorized {
			handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		} else {
			h.logger.Error("Error fetching job", zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error fetching job", err)
		}
		return
	}

	// Job polling is stateful; stale cached responses can report an old progress payload after
	// the import itself completed, causing clients to lose the terminal imported/skipped counts.
	w.Header().Set("Cache-Control", "no-store")
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}
