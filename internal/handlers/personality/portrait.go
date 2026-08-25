package personality

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// GenerateFlowPortrait POST enqueues a personality_portrait job for the wizard review screen.
func (h *Handler) GenerateFlowPortrait(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	flowID, err := parseFlowID(r)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid flow ID", err)
		return
	}

	if h.personalityAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Agent not configured", nil)
		return
	}

	flow, err := h.ds.GetFlow(r.Context(), userID, flowID)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("portrait job: get flow", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load flow", err)
		return
	}
	if flow.GeneratedPrompt == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Flow has not been generated yet", nil)
		return
	}
	if flow.ImageStyle == models.ImageStyleNone {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Portrait generation is disabled for this style", nil)
		return
	}
	if flow.ReferenceImageID != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "A reference image is already set; portrait generation is not needed", nil)
		return
	}

	job, err := h.personalityAgent.EnqueuePersonalityPortraitJob(r.Context(), userID, flowID, flow.GeneratedPrompt, flow.ImageStyle)
	if err != nil {
		h.respondEnqueueError(w, r, err, "A personality media job is already in progress")
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.PersonalityMediaJobResponse{
		JobID:   job.ID.String(),
		JobType: agent.JobTypePersonalityPortrait,
	})
}
