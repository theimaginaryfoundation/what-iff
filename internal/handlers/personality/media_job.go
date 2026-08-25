package personality

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// GetActiveMediaJob returns the user's in-flight expression_grid or personality_portrait job, if any.
func (h *Handler) GetActiveMediaJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	active, err := h.ds.FindActivePersonalityMediaJob(r.Context(), userID)
	if err != nil {
		h.logger.Error("active personality media job lookup failed", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load active job", err)
		return
	}
	if active == nil {
		handlerutils.RespondWithNoContent(w)
		return
	}

	payload, err := h.buildActivePersonalityMediaJob(r.Context(), userID, active)
	if err != nil {
		h.logger.Error("active personality media job enrichment failed", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load active job", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, payload)
}

func (h *Handler) buildActivePersonalityMediaJob(ctx context.Context, userID uuid.UUID, job *models.Job) (models.ActivePersonalityMediaJob, error) {
	out := models.ActivePersonalityMediaJob{
		JobID:     job.ID.String(),
		JobType:   job.JobType,
		Reference: job.Reference,
		Status:    string(job.Status),
		Error:     job.Error,
	}
	refID, err := uuid.Parse(job.Reference)
	if err != nil {
		return out, nil
	}
	switch job.JobType {
	case agent.JobTypeExpressionGrid:
		pid := refID.String()
		out.PersonalityID = &pid
		if person, err := h.ds.GetPersonality(ctx, userID, refID); err == nil && person != nil {
			name := person.Name
			out.PersonalityName = &name
		}
	case agent.JobTypePersonalityPortrait:
		fallthrough
	case agent.JobTypePersonalityGeneration:
		fid := refID.String()
		out.FlowID = &fid
	}
	return out, nil
}

func (h *Handler) respondPersonalityJobConflict(w http.ResponseWriter, r *http.Request, active *models.Job, message string) {
	userID, _ := middleware.GetUserIDFromContext(r.Context())
	enriched, err := h.buildActivePersonalityMediaJob(r.Context(), userID, active)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, message, &agent.ErrPersonalityMediaJobActive{Job: active})
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusConflict, models.PersonalityMediaJobConflict{
		Message: message,
		Active:  enriched,
	})
}

func (h *Handler) respondEnqueueError(w http.ResponseWriter, r *http.Request, err error, conflictMessage string) {
	var activeErr *agent.ErrPersonalityMediaJobActive
	if errors.As(err, &activeErr) && activeErr.Job != nil {
		h.respondPersonalityJobConflict(w, r, activeErr.Job, conflictMessage)
		return
	}
	h.logger.Error("failed to enqueue personality media job", zap.Error(err))
	handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to start job", err)
}
