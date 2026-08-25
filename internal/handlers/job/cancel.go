package job

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// CancelJob handles cancellation requests for in-progress jobs.
func (h *Handler) CancelJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("Failed to get user ID from context")
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Job ID is required", nil)
		return
	}

	jobID, err := uuid.Parse(idStr)
	if err != nil {
		h.logger.Error("Invalid job ID", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid job ID", err)
		return
	}

	jobModel, err := h.provider.GetJob(r.Context(), userID, jobID)
	if err != nil {
		if err == datastore.ErrJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Job not found", nil)
		} else if err == datastore.ErrUnauthorized {
			handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		} else {
			h.logger.Error("Error fetching job for cancel", zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error cancelling job", err)
		}
		return
	}

	if jobModel.Status == models.JobStatusComplete || jobModel.Status == models.JobStatusCancelled || jobModel.Status == models.JobStatusFailed {
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, jobModel)
		return
	}
	if h.canceller == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet, "Job cancellation is not available", nil)
		return
	}
	if err := h.canceller.CancelJob(r.Context(), userID, jobID); err != nil {
		if err == datastore.ErrUnauthorized {
			handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
			return
		}
		h.logger.Error("Error triggering job cancel", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error cancelling job", err)
		return
	}

	latest, err := h.provider.GetJob(r.Context(), userID, jobID)
	if err != nil {
		h.logger.Error("Error fetching job after cancel", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error cancelling job", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, latest)
}
