package personality

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// GenerateDefaultExpressionGrid POST enqueues a background expression_grid job (HTTP 202).
// When the default grid is already complete and force is not set, returns the existing expressions (HTTP 200).
//
// Query: force=true — regenerate even when all default keys already have images.
func (h *Handler) GenerateDefaultExpressionGrid(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.expressionRouteIDs(w, r)
	if !ok {
		return
	}

	if h.personalityAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Agent not configured", nil)
		return
	}

	force := strings.EqualFold(r.URL.Query().Get("force"), "true")

	existing, err := h.ds.ListPersonalityExpressions(r.Context(), userID, personalityID)
	if err != nil {
		if errors.Is(err, datastore.ErrPersonalityNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
			return
		}
		h.logger.Error("expression grid: list expressions before generate",
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load expressions", err)
		return
	}
	if !force && completeDefaultExpressionGrid(existing) {
		w.Header().Set("X-Expression-Grid", "unchanged")
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, existing)
		return
	}

	job, err := h.personalityAgent.EnqueueExpressionGridJob(r.Context(), userID, personalityID)
	if err != nil {
		h.respondEnqueueError(w, r, err, "A personality media job is already in progress")
		return
	}

	handlerutils.RefreshResponseWriteDeadline(w, 5*time.Second)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.PersonalityMediaJobResponse{
		JobID:   job.ID.String(),
		JobType: agent.JobTypeExpressionGrid,
	})
}
