package personality

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// expressionCandidateCount is fixed by the 3×3 grid the image model renders.
const expressionCandidateCount = 9

type generateExpressionCandidatesRequest struct {
	// Expressions are nine URL-safe expression keys in row-major grid order.
	Expressions []string `json:"expressions"`
	// ReferenceImageID optionally names an owned gallery image used as a style/character reference.
	ReferenceImageID *string `json:"reference_image_id"`
}

// GenerateExpressionCandidates POST enqueues a background expression_grid job (HTTP 202) that renders
// nine portraits for caller-chosen keys without assigning them. The completed job's `progress`
// carries the candidate image IDs; clients assign keepers via PUT /personality/{id}/expressions/{key}.
func (h *Handler) GenerateExpressionCandidates(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.expressionRouteIDs(w, r)
	if !ok {
		return
	}

	if h.personalityAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Agent not configured", nil)
		return
	}

	var req generateExpressionCandidatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	keys, err := validateExpressionCandidateKeys(req.Expressions)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}
	var referenceImageID *uuid.UUID
	if req.ReferenceImageID != nil && *req.ReferenceImageID != "" {
		id, err := uuid.Parse(*req.ReferenceImageID)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid reference_image_id", err)
			return
		}
		referenceImageID = &id
	}

	job, err := h.personalityAgent.EnqueueExpressionCandidatesJob(r.Context(), userID, personalityID, keys, referenceImageID)
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrPersonalityNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", nil)
		case errors.Is(err, agent.ErrExpressionReferenceImageNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Reference image not found", nil)
		case errors.Is(err, agent.ErrExpressionCandidateKeyCount):
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, fmt.Sprintf("expressions must contain exactly %d keys", expressionCandidateCount), nil)
		case errors.Is(err, agent.ErrExpressionImagesDisabled):
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Image generation is disabled for this personality (image style is none)", nil)
		default:
			h.respondEnqueueError(w, r, err, "A personality media job is already in progress")
		}
		return
	}

	handlerutils.RefreshResponseWriteDeadline(w, 5*time.Second)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.PersonalityMediaJobResponse{
		JobID:   job.ID.String(),
		JobType: agent.JobTypeExpressionGrid,
	})
}

// validateExpressionCandidateKeys requires exactly nine unique, URL-safe keys.
func validateExpressionCandidateKeys(keys []string) ([]string, error) {
	if len(keys) != expressionCandidateCount {
		return nil, fmt.Errorf("expressions must contain exactly %d keys", expressionCandidateCount)
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if !isValidExpressionKey(key) {
			return nil, fmt.Errorf("invalid expression key %q", key)
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("duplicate expression key %q", key)
		}
		seen[key] = struct{}{}
	}
	return keys, nil
}
