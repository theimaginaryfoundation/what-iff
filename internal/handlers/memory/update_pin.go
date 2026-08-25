package memory

import (
	"encoding/json"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// UpdateMemoryPinRequest represents the request body for updating a memory's pinned personality
type UpdateMemoryPinRequest struct {
	PinnedPersonalityID *string `json:"pinned_personality_id"`
}

// UpdateMemoryPin updates the pinned personality for a User-scoped memory
func (h *Handler) UpdateMemoryPin(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get memory ID from URL
	vars := mux.Vars(r)
	memoryIDStr, ok := vars["id"]
	if !ok || memoryIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Memory ID is required", nil)
		return
	}

	memoryID, err := uuid.Parse(memoryIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid memory ID", err)
		return
	}

	// Parse request body
	var req UpdateMemoryPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Parse the pinned personality ID if provided
	var pinnedPersonalityID *uuid.UUID
	if req.PinnedPersonalityID != nil && *req.PinnedPersonalityID != "" {
		parsedID, err := uuid.Parse(*req.PinnedPersonalityID)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
			return
		}
		pinnedPersonalityID = &parsedID
	}

	// Update the memory pin
	updatedMemory, err := h.ds.UpdateMemoryPin(r.Context(), userID, memoryID, pinnedPersonalityID)
	if ent.IsNotFound(err) || err == datastore.ErrMemoryNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Memory not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to update memory pin",
			zap.String("user_id", userID.String()),
			zap.String("memory_id", memoryID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update memory pin", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updatedMemory)
}
