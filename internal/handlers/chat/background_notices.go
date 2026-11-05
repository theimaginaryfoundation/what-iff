package chat

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

// ListBackgroundNotices returns the chat's background work that failed or fell back and that
// the account hasn't dismissed (ADR 0x027 section 6). Each notice states the step and its
// cause; what to tell the person, and how to fix it, is the client's.
func (h *Handler) ListBackgroundNotices(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, err := uuid.Parse(mux.Vars(r)["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}
	notices, err := h.ds.ListBackgroundNotices(r.Context(), userID, chatID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "failed to list background notices", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, notices)
}

// DismissBackgroundNotice hides a notice until its cause changes.
func (h *Handler) DismissBackgroundNotice(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, err := uuid.Parse(mux.Vars(r)["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}
	noticeID, err := uuid.Parse(mux.Vars(r)["noticeId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid notice ID", err)
		return
	}
	err = h.ds.DismissBackgroundNotice(r.Context(), userID, chatID, noticeID)
	if errors.Is(err, datastore.ErrBackgroundNoticeNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Notice not found", nil)
		return
	}
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "failed to dismiss the notice", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
