package memory

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ListCompactionEvents returns paginated compaction (checkpoint) audit records, newest first, each
// with its summary/scratchpad snapshots and nested merge events resolved.
func (h *Handler) ListCompactionEvents(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ClampIntParam(queryParams.Get("limit"), 20, 1, 100)
	var chatID, personalityID *uuid.UUID
	if raw := queryParams.Get("chat_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
			return
		}
		chatID = &id
	}
	if raw := queryParams.Get("personality_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
			return
		}
		personalityID = &id
	}

	events, err := h.ds.ListCompactionEvents(r.Context(), userID, page, pageSize, chatID, personalityID)
	if err != nil {
		h.logger.Error("failed to list compaction events",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list compaction events", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, events)
}

// GetCompactionEvent returns a single compaction event with all snapshots and merge events resolved.
func (h *Handler) GetCompactionEvent(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	eventID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid compaction event ID", err)
		return
	}

	event, err := h.ds.GetCompactionEvent(r.Context(), userID, eventID)
	if err != nil {
		if errors.Is(err, datastore.ErrCompactionEventNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Compaction event not found", err)
			return
		}
		h.logger.Error("failed to get compaction event",
			zap.String("user_id", userID.String()),
			zap.String("event_id", eventID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get compaction event", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, event)
}

// RevertCheckpointSnapshot restores a summary or scratchpad snapshot to the live value it came from.
func (h *Handler) RevertCheckpointSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	snapshotID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid snapshot ID", err)
		return
	}

	snapshot, err := h.ds.RevertCheckpointSnapshot(r.Context(), userID, snapshotID)
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrCheckpointSnapshotNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Snapshot not found", err)
		case errors.Is(err, datastore.ErrCheckpointSnapshotOwnerMissing):
			handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, handlerutils.CodeNotSet, "Snapshot cannot be reverted (missing owner reference)", err)
		case errors.Is(err, datastore.ErrChatNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat for summary snapshot not found", err)
		case errors.Is(err, datastore.ErrPersonalityNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality for scratchpad snapshot not found", err)
		default:
			h.logger.Error("failed to revert checkpoint snapshot",
				zap.String("user_id", userID.String()),
				zap.String("snapshot_id", snapshotID.String()),
				zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to revert snapshot", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, snapshot)
}
