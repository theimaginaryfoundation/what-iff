package personality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type personalityPromptHistoryStore interface {
	UpdatePersonalityWithPromptHistory(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	ListPersonalityPromptChanges(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityPromptChange, error)
	RevertPersonalityPromptChange(ctx context.Context, userID, personalityID, changeID uuid.UUID) (*models.PersonalityPromptChange, error)
}

// UpdatePersonalityWithPromptHistory is the user-facing personality PUT path.
// Datastores that implement the history-aware seam persist the prompt transition
// atomically with the personality update. Test doubles that predate this seam
// continue through the existing handler behavior.
func (h *Handler) UpdatePersonalityWithPromptHistory(w http.ResponseWriter, r *http.Request) {
	store, ok := h.ds.(personalityPromptHistoryStore)
	if !ok {
		h.UpdatePersonality(w, r)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	personalityID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}

	var req models.Personality
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Personality name is required", nil)
		return
	}
	if req.SystemPrompt == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "System prompt is required", nil)
		return
	}
	if handlerutils.UTF16CodeUnitCount(req.SystemPrompt) > handlerutils.TextLimitHardMax {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, models.ErrCodeSystemPromptTooLong,
			fmt.Sprintf("System prompt exceeds maximum length of %d characters", handlerutils.TextLimitHardMax), nil)
		return
	}
	if err := normalizeAndValidateStyling(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}
	req.ID = personalityID

	updated, err := store.UpdatePersonalityWithPromptHistory(r.Context(), userID, req)
	if ent.IsNotFound(err) || errors.Is(err, datastore.ErrPersonalityNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Cover image not found", nil)
		return
	} else if err != nil {
		h.logger.Error("failed to update personality with prompt history",
			zap.String("user_id", userID.String()),
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update personality", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updated)
}

func (h *Handler) ListPersonalityPromptChanges(w http.ResponseWriter, r *http.Request) {
	store, ok := h.ds.(personalityPromptHistoryStore)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet, "Personality prompt history is unavailable", nil)
		return
	}
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	personalityID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}
	changes, err := store.ListPersonalityPromptChanges(r.Context(), userID, personalityID)
	if errors.Is(err, datastore.ErrPersonalityNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list personality prompt changes", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, changes)
}

func (h *Handler) RevertPersonalityPromptChange(w http.ResponseWriter, r *http.Request) {
	store, ok := h.ds.(personalityPromptHistoryStore)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet, "Personality prompt history is unavailable", nil)
		return
	}
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	vars := mux.Vars(r)
	personalityID, err := uuid.Parse(vars["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}
	changeID, err := uuid.Parse(vars["change_id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid prompt change ID", err)
		return
	}
	change, err := store.RevertPersonalityPromptChange(r.Context(), userID, personalityID, changeID)
	if errors.Is(err, datastore.ErrPersonalityNotFound) || errors.Is(err, datastore.ErrPersonalityPromptChangeNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality prompt change not found", err)
		return
	} else if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to revert personality prompt change", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, change)
}
