package personality

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// GetOrCreateFlow returns the user's active (in_progress or generated) personality gen flow, creating one if none exists.
func (h *Handler) GetOrCreateFlow(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	flow, err := h.ds.GetOrCreateActiveFlow(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get or create gen flow", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load personality generation flow", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, flow)
}

// GetFlow returns a specific personality generation flow by ID.
func (h *Handler) GetFlow(w http.ResponseWriter, r *http.Request) {
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

	flow, err := h.ds.GetFlow(r.Context(), userID, flowID)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("failed to get gen flow", zap.String("user_id", userID.String()), zap.String("flow_id", flowID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load personality generation flow", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, flow)
}

// UpdateFlow saves the user's partial wizard progress.
func (h *Handler) UpdateFlow(w http.ResponseWriter, r *http.Request) {
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

	var req models.UpdateFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	flow, err := h.ds.UpdateFlow(r.Context(), userID, flowID, req)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("failed to update gen flow", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to save progress", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, flow)
}

// ResetFlow abandons the current draft flow and returns a fresh empty flow.
func (h *Handler) ResetFlow(w http.ResponseWriter, r *http.Request) {
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

	flow, err := h.ds.ResetFlow(r.Context(), userID, flowID)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		if err == datastore.ErrFlowGenerationJobAlreadyActive {
			active, activeErr := h.ds.FindActivePersonalityGenerationJob(r.Context(), userID, flowID)
			if activeErr == nil && active != nil {
				h.respondPersonalityJobConflict(w, r, active, "A personality generation job is already in progress")
				return
			}
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "A personality generation job is already in progress", err)
			return
		}
		h.logger.Error("failed to reset gen flow",
			zap.String("user_id", userID.String()),
			zap.String("flow_id", flowID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to reset flow", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, flow)
}

// CompleteFlow enqueues async personality generation from collected answers.
func (h *Handler) CompleteFlow(w http.ResponseWriter, r *http.Request) {
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

	// Fetch the flow to get answers.
	flow, err := h.ds.GetFlow(r.Context(), userID, flowID)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("failed to get gen flow", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load flow", err)
		return
	}

	if len(flow.Answers) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "No answers provided yet", nil)
		return
	}

	if h.personalityAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Agent not configured", nil)
		return
	}

	job, err := h.personalityAgent.EnqueuePersonalityGenerationJob(r.Context(), userID, flowID)
	if err != nil {
		h.respondEnqueueError(w, r, err, "A personality generation job is already in progress")
		return
	}
	h.logger.Info("personality generation request enqueued",
		zap.String("job_id", job.ID.String()),
		zap.String("user_id", userID.String()),
		zap.String("flow_id", flowID.String()),
		zap.String("job_type", job.JobType))

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.PersonalityMediaJobResponse{
		JobID:   job.ID.String(),
		JobType: job.JobType,
	})
}

// RegenerateFlow re-runs generation asynchronously for an existing flow.
func (h *Handler) RegenerateFlow(w http.ResponseWriter, r *http.Request) {
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

	flow, err := h.ds.GetFlow(r.Context(), userID, flowID)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("failed to get gen flow for regenerate", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load flow", err)
		return
	}
	if len(flow.Answers) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "No answers provided yet", nil)
		return
	}
	if flow.Status != "generated" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Flow is not in generated state", nil)
		return
	}

	req := models.UpdateFlowRequest{
		CurrentStep:      flow.CurrentStep,
		Answers:          flow.Answers,
		ImageStyle:       flow.ImageStyle,
		ReferenceImageID: flow.ReferenceImageID,
	}
	if _, err := h.ds.UpdateFlow(r.Context(), userID, flowID, req); err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("failed to prepare flow for regenerate", zap.String("user_id", userID.String()), zap.String("flow_id", flowID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to prepare flow for regeneration", err)
		return
	}

	if h.personalityAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Agent not configured", nil)
		return
	}
	job, err := h.personalityAgent.EnqueuePersonalityGenerationJob(r.Context(), userID, flowID)
	if err != nil {
		h.respondEnqueueError(w, r, err, "A personality generation job is already in progress")
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.PersonalityMediaJobResponse{
		JobID:   job.ID.String(),
		JobType: job.JobType,
	})
}

// GetActiveGenerationJob returns the user's active personality_generation job for the given flow.
func (h *Handler) GetActiveGenerationJob(w http.ResponseWriter, r *http.Request) {
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

	active, err := h.ds.FindActivePersonalityGenerationJob(r.Context(), userID, flowID)
	if err != nil {
		h.logger.Error("active personality generation job lookup failed", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load active job", err)
		return
	}
	if active == nil {
		handlerutils.RespondWithNoContent(w)
		return
	}

	payload, err := h.buildActivePersonalityMediaJob(r.Context(), userID, active)
	if err != nil {
		h.logger.Error("active personality generation job enrichment failed", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load active job", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, payload)
}

// AcceptFlow creates a real Personality from the generated flow and links them.
func (h *Handler) AcceptFlow(w http.ResponseWriter, r *http.Request) {
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

	flow, err := h.ds.GetFlow(r.Context(), userID, flowID)
	if err != nil {
		if err == datastore.ErrFlowNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Flow not found", err)
			return
		}
		h.logger.Error("failed to get gen flow for accept", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load flow", err)
		return
	}

	if flow.GeneratedPrompt == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Flow has not been generated yet", nil)
		return
	}

	// Accept an optional name override from the request body.
	var acceptReq models.AcceptFlowRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&acceptReq); err != nil && !errors.Is(err, io.EOF) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Name priority: request body > user-provided answer > first generated name > fallback.
	name := acceptReq.Name
	if name == "" {
		name = flow.Answers["name"]
	}
	if name == "" && len(flow.GeneratedNames) > 0 {
		name = flow.GeneratedNames[0]
	}

	// Check if this is the user's first custom personality (for auto-default behavior).
	isFirstPersonality := false
	if page, err := h.ds.ListPersonalities(r.Context(), userID, 1, 1, models.PersonalityFilters{}); err == nil {
		isFirstPersonality = page.TotalCount == 0
	}

	// Create the personality.
	personalityReq := models.Personality{
		Name:               name,
		SystemPrompt:       flow.GeneratedPrompt,
		ImageStyle:         flow.ImageStyle,
		ExpressionsEnabled: true,
	}
	// Cover image priority: explicit accept override > reference image from wizard > nothing.
	if acceptReq.CoverImageID != nil {
		personalityReq.CoverImageID = acceptReq.CoverImageID
	} else if flow.ReferenceImageID != nil {
		personalityReq.CoverImageID = flow.ReferenceImageID
	}

	personality, err := h.ds.CreatePersonality(r.Context(), userID, personalityReq)
	if err != nil {
		h.logger.Error("failed to create personality from flow",
			zap.String("user_id", userID.String()),
			zap.String("flow_id", flowID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create personality", err)
		return
	}

	// Auto-set as default if first personality.
	if isFirstPersonality && personality != nil {
		prefs, err := h.ds.GetUserPreferences(r.Context(), userID)
		if err != nil {
			h.logger.Warn("failed to fetch user preferences for auto-default personality",
				zap.String("user_id", userID.String()),
				zap.String("personality_id", personality.ID.String()),
				zap.Error(err))
		} else {
			prefs.DefaultPersonalityID = personality.ID
			if _, err := h.ds.UpdateUserPreferences(r.Context(), userID, *prefs); err != nil {
				h.logger.Warn("failed to update user preferences for auto-default personality",
					zap.String("user_id", userID.String()),
					zap.String("personality_id", personality.ID.String()),
					zap.Error(err))
			}
		}
	}

	// Link the flow to the personality.
	_, err = h.ds.AcceptFlow(r.Context(), userID, flowID, personality.ID)
	if err != nil {
		h.logger.Error("failed to mark flow as accepted after creating personality",
			zap.String("user_id", userID.String()),
			zap.String("flow_id", flowID.String()),
			zap.String("personality_id", personality.ID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to finalize personality creation", err)
		return
	}

	if h.personalityAgent != nil && flow.ImageStyle != models.ImageStyleNone {
		if _, err := h.personalityAgent.EnqueueExpressionGridJob(r.Context(), userID, personality.ID); err != nil {
			var busy *agent.ErrPersonalityMediaJobActive
			if !errors.As(err, &busy) {
				h.logger.Warn("accept flow: failed to enqueue default expression grid",
					zap.String("personality_id", personality.ID.String()),
					zap.Error(err))
			}
		}
	}

	handlerutils.RefreshResponseWriteDeadline(w, 60*time.Second)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, personality)
}

func parseFlowID(r *http.Request) (uuid.UUID, error) {
	vars := mux.Vars(r)
	return uuid.Parse(vars["id"])
}
