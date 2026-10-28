package user

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

type setCurrentTimezoneRequest struct {
	Timezone string `json:"timezone"`
}

// SetCurrentTimezone records the timezone the user's browser reports. Jobs that follow the user
// run in it; the user's own (home) timezone is unchanged. The answer says whether following is
// paused, and whether this report paused it (the user's devices kept disagreeing).
func (h *Handler) SetCurrentTimezone(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req setCurrentTimezoneRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}

	report, err := h.store.SetUserCurrentTimezone(r.Context(), userID, strings.TrimSpace(req.Timezone))
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrInvalidTimezone):
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", nil)
		case errors.Is(err, datastore.ErrUserNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error updating current timezone", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, report)
}

// ResumeTimezoneFollowing ends a pause of following the user's timezone; the browser's next report
// sets it again.
func (h *Handler) ResumeTimezoneFollowing(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if err := h.store.ResumeTimezoneFollowing(r.Context(), userID); err != nil {
		if errors.Is(err, datastore.ErrUserNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error resuming timezone following", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
