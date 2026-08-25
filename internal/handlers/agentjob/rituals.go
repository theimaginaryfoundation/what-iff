package agentjob

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

// AddAgentJobRitual attaches a ritual to an agent job.
func (h *Handler) AddAgentJobRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	jobID, ritualID, ok := parseJobRitualIDs(w, r, h.logger)
	if !ok {
		return
	}

	if err := h.provider.AddAgentJobRitual(r.Context(), userID, jobID, ritualID); err != nil {
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err == datastore.ErrRitualNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Skill not found", nil)
			return
		}
		h.logger.Error("failed to add ritual to agent job",
			zap.String("user_id", userID.String()),
			zap.String("job_id", jobID.String()),
			zap.String("ritual_id", ritualID.String()),
			zap.Error(err),
		)
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to add ritual", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveAgentJobRitual detaches a ritual from an agent job.
func (h *Handler) RemoveAgentJobRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	jobID, ritualID, ok := parseJobRitualIDs(w, r, h.logger)
	if !ok {
		return
	}

	if err := h.provider.RemoveAgentJobRitual(r.Context(), userID, jobID, ritualID); err != nil {
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err == datastore.ErrRitualNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Skill not found", nil)
			return
		}
		h.logger.Error("failed to remove ritual from agent job",
			zap.String("user_id", userID.String()),
			zap.String("job_id", jobID.String()),
			zap.String("ritual_id", ritualID.String()),
			zap.Error(err),
		)
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to remove ritual", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseJobRitualIDs(w http.ResponseWriter, r *http.Request, logger *zap.Logger) (jobID, ritualID uuid.UUID, ok bool) {
	vars := mux.Vars(r)

	idStr := vars["id"]
	if idStr == "" {
		handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, "AgentJob ID is required", nil)
		return
	}
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid AgentJob ID", err)
		return
	}

	ritualIDStr := vars["ritualId"]
	if ritualIDStr == "" {
		handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}
	ritualID, err = uuid.Parse(ritualIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid Ritual ID", err)
		return
	}

	return jobID, ritualID, true
}
