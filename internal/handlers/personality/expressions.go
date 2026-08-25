package personality

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const maxExpressionLabelLength = 80

var personalityExpressionKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (h *Handler) ListExpressions(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.expressionRouteIDs(w, r)
	if !ok {
		return
	}

	expressions, err := h.ds.ListPersonalityExpressions(r.Context(), userID, personalityID)
	if err != nil {
		h.respondExpressionDatastoreError(w, "Failed to list personality expressions", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, expressions)
}

func (h *Handler) UpsertExpression(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.expressionRouteIDs(w, r)
	if !ok {
		return
	}

	expressionKey := mux.Vars(r)["expression_key"]
	if !isValidExpressionKey(expressionKey) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid expression key", nil)
		return
	}

	req, err := parseUpdateExpressionRequest(r.Body)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	expression, err := h.ds.UpsertPersonalityExpression(r.Context(), userID, personalityID, expressionKey, req)
	if err != nil {
		h.respondExpressionDatastoreError(w, "Failed to save personality expression", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, expression)
}

func (h *Handler) DeleteExpression(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.expressionRouteIDs(w, r)
	if !ok {
		return
	}

	expressionKey := mux.Vars(r)["expression_key"]
	if !isValidExpressionKey(expressionKey) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid expression key", nil)
		return
	}

	if err := h.ds.DeletePersonalityExpression(r.Context(), userID, personalityID, expressionKey); err != nil {
		h.respondExpressionDatastoreError(w, "Failed to delete personality expression", err)
		return
	}

	handlerutils.RespondWithNoContent(w)
}

func (h *Handler) expressionRouteIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, uuid.Nil, false
	}

	personalityIDStr := mux.Vars(r)["id"]
	personalityID, err := uuid.Parse(personalityIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return uuid.Nil, uuid.Nil, false
	}

	return userID, personalityID, true
}

func (h *Handler) respondExpressionDatastoreError(w http.ResponseWriter, message string, err error) {
	switch {
	case errors.Is(err, datastore.ErrPersonalityNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", nil)
	case errors.Is(err, datastore.ErrPersonalityExpressionNotDeletable):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "This expression cannot be deleted", err)
	case errors.Is(err, datastore.ErrFileAttachmentNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Image not found", nil)
	default:
		h.logger.Error(message, zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, message, err)
	}
}

func parseUpdateExpressionRequest(body io.Reader) (models.UpdatePersonalityExpressionRequest, error) {
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return models.UpdatePersonalityExpressionRequest{}, err
	}

	var req models.UpdatePersonalityExpressionRequest

	if raw, ok := payload["image_id"]; ok {
		req.ImageSet = true
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var imageIDStr string
			if err := json.Unmarshal(raw, &imageIDStr); err != nil {
				return models.UpdatePersonalityExpressionRequest{}, fmt.Errorf("image_id must be a UUID string or null")
			}
			imageID, err := uuid.Parse(imageIDStr)
			if err != nil {
				return models.UpdatePersonalityExpressionRequest{}, fmt.Errorf("invalid image_id: %w", err)
			}
			req.ImageID = &imageID
		}
	}

	if raw, ok := payload["label"]; ok {
		req.LabelSet = true
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var label string
			if err := json.Unmarshal(raw, &label); err != nil {
				return models.UpdatePersonalityExpressionRequest{}, fmt.Errorf("label must be a string or null")
			}
			if len(label) > maxExpressionLabelLength {
				return models.UpdatePersonalityExpressionRequest{}, fmt.Errorf("label must be at most %d characters", maxExpressionLabelLength)
			}
			req.Label = &label
		}
	}

	if !req.ImageSet && !req.LabelSet {
		return models.UpdatePersonalityExpressionRequest{}, fmt.Errorf("image_id or label is required")
	}

	return req, nil
}

func isValidExpressionKey(key string) bool {
	return personalityExpressionKeyPattern.MatchString(key)
}
