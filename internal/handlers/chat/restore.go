package chat

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// RestoreChat unarchives a thread after the user's explicit confirmation, optionally preparing
// a fresh working summary guided by their own words. Reading an archived thread never calls it.
func (h *Handler) RestoreChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}
	var req struct {
		Guidance string `json:"guidance"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid restore request", err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Expected one restore request", nil)
		return
	}
	guidance, err := modeltypes.NormalizeRestoreGuidance(req.Guidance)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}
	if _, err := h.ds.GetChat(r.Context(), userID, chatID); err != nil {
		status, message := http.StatusInternalServerError, "Failed to load thread"
		if ent.IsNotFound(err) || errors.Is(err, datastore.ErrChatNotFound) {
			status, message = http.StatusNotFound, "Chat not found"
		}
		handlerutils.RespondWithError(w, h.logger, status, handlerutils.CodeNotSet, message, err)
		return
	}
	chat, err := h.ds.RestoreChat(r.Context(), userID, chatID, guidance)
	if err != nil {
		status, message := http.StatusInternalServerError, "Failed to unarchive thread"
		if errors.Is(err, datastore.ErrChatRestoreConflict) {
			status, message = http.StatusConflict, err.Error()
		}
		handlerutils.RespondWithError(w, h.logger, status, handlerutils.CodeNotSet, message, err)
		return
	}
	// A filled-in restore dialog is explicit intent to prepare this context now. Blank restores
	// retain the existing lazy behavior and do not create a summary job merely by unarchiving.
	if guidance != "" {
		h.startRehydrationIfNeeded(r.Context(), userID, chat)
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, chat)
}
