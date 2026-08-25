package memory

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// DeleteMemory deletes a memory by ID
func (h *Handler) DeleteMemory(w http.ResponseWriter, r *http.Request) {
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

	// Delete memory
	err = h.ds.DeleteMemory(r.Context(), userID, memoryID)
	if ent.IsNotFound(err) || err == datastore.ErrMemoryNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Memory not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to delete memory",
			zap.String("user_id", userID.String()),
			zap.String("memory_id", memoryID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete memory", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
