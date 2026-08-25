package ritual

import (
	"encoding/json"
	"net/http"

	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

type bindingRequest struct {
	Hotkeys string `json:"hotkeys"`
}

// UpsertBinding creates or updates a system ritual binding for the authenticated user.
func (h *Handler) UpsertBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}

	ritualID, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}

	// Only allow system rituals
	if !agent.IsSystemRitual(ritualID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Only system rituals can be bound via this endpoint", nil)
		return
	}

	var req bindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate hotkeys server-side
	hotkeys := req.Hotkeys
	trimmed := strings.TrimSpace(hotkeys)
	// If empty -> treat as delete
	if trimmed == "" {
		if err := h.getBindingStore().DeleteSystemBinding(r.Context(), userID, ritualID); err != nil {
			h.logger.Error("failed to delete system ritual binding", zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete binding", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := ValidateHotkey(hotkeys); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid hotkey format", err)
		return
	}

	binding, err := h.getBindingStore().UpsertSystemBinding(r.Context(), userID, ritualID, hotkeys)
	if err != nil {
		if errors.Is(err, datastore.ErrHotkeyConflict) {
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Hotkey already in use", err)
			return
		}
		h.logger.Error("failed to upsert system ritual binding", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to save binding", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, binding)
}

// DeleteBinding removes a system ritual binding for the authenticated user.
func (h *Handler) DeleteBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}

	ritualID, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}

	// Only allow system rituals
	if !agent.IsSystemRitual(ritualID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Only system rituals can be unbound via this endpoint", nil)
		return
	}

	if err := h.getBindingStore().DeleteSystemBinding(r.Context(), userID, ritualID); err != nil {
		h.logger.Error("failed to delete system ritual binding", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete binding", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBinding returns the current user's binding for a system ritual.
func (h *Handler) GetBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}

	ritualID, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}

	// Only allow system rituals
	if !agent.IsSystemRitual(ritualID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Only system rituals have bindings here", nil)
		return
	}

	bindings, err := h.getBindingStore().GetSystemBindingsForUser(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to load bindings", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load binding", err)
		return
	}

	for _, b := range bindings {
		if b.RitualID == ritualID {
			handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, b)
			return
		}
	}

	// Not found
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]interface{}{})
}
