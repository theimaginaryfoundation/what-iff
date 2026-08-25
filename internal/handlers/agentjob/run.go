package agentjob

import (
	"errors"
	"net/http"

	agentjobscheduler "github.com/theimaginaryfoundation/what-iff/internal/agentjobs/scheduler"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type runAgentJobNowResponse struct {
	Status string `json:"status"`
}

func (h *Handler) RunAgentJobNow(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "AgentJob ID is required", nil)
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid AgentJob ID", err)
		return
	}

	job, err := h.provider.GetAgentJob(r.Context(), userID, id)
	if err == datastore.ErrAgentJobNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
		return
	}
	if err != nil {
		h.logger.Error("failed to get agent job for manual run",
			zap.String("user_id", userID.String()),
			zap.String("agent_job_id", id.String()),
			zap.Error(err),
		)
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load agent job", err)
		return
	}

	if job.Status != models.AgentJobStatusActive && job.Status != models.AgentJobStatusPaused {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "AgentJob can only be run when status is 'active' or 'paused'", nil)
		return
	}

	if h.runner == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "AgentJob scheduler is not enabled", nil)
		return
	}

	if err := h.runner.RunAgentJobNow(r.Context(), userID, id); err != nil {
		if errors.Is(err, agentjobscheduler.ErrSchedulerNotActive) {
			handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "AgentJob scheduler is not active", nil)
			return
		}
		h.logger.Error("failed to trigger agent job run now",
			zap.String("user_id", userID.String()),
			zap.String("agent_job_id", id.String()),
			zap.Error(err),
		)
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to trigger agent job", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, runAgentJobNowResponse{Status: "triggered"})
}
