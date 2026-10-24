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
// run in it; the user's own (home) timezone is unchanged.
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

	if _, err := h.store.SetUserCurrentTimezone(r.Context(), userID, strings.TrimSpace(req.Timezone)); err != nil {
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

	w.WriteHeader(http.StatusNoContent)
}
