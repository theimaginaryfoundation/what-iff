package agentjob

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

func (h *Handler) GetAgentJob(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Error("failed to get agent job", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get agent job", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}
