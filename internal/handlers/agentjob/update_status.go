package agentjob

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type updateAgentJobStatusRequest struct {
	Status models.AgentJobStatus `json:"status"`
}

func (h *Handler) UpdateAgentJobStatus(w http.ResponseWriter, r *http.Request) {
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

	var req updateAgentJobStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	req.Status = models.AgentJobStatus(strings.TrimSpace(string(req.Status)))
	if req.Status != models.AgentJobStatusActive && req.Status != models.AgentJobStatusPaused {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid status (must be 'active' or 'paused')", nil)
		return
	}

	job, err := h.provider.UpdateAgentJobStatus(r.Context(), userID, id, req.Status, "")
	if err == datastore.ErrAgentJobNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
		return
	}
	if err != nil {
		h.logger.Error("failed to update agent job status", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update agent job status", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}
