package memory

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ListMemoryMergeEvents returns paginated merge audit rows (page and limit only; ordered by created_at desc).
func (h *Handler) ListMemoryMergeEvents(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 20)

	events, err := h.ds.ListMemoryMergeEvents(r.Context(), userID, page, pageSize, models.MemoryMergeEventFilters{})
	if err != nil {
		h.logger.Error("failed to list memory merge events",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list memory merge events", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, events)
}

func (h *Handler) UndoMemoryMergeEvent(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	eventID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid merge event ID", err)
		return
	}

	event, err := h.ds.UndoMemoryMergeEvent(r.Context(), userID, eventID)
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrMemoryMergeEventNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Merge event not found", err)
		case errors.Is(err, datastore.ErrMemoryMergeAlreadyReverted):
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Merge event already reverted", err)
		case errors.Is(err, datastore.ErrMemoryNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Survivor memory not found", err)
		default:
			h.logger.Error("failed to undo memory merge event",
				zap.String("user_id", userID.String()),
				zap.String("event_id", eventID.String()),
				zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to undo merge event", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, event)
}
