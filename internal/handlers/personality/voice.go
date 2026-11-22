package personality

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// SetVoice sets the personality's voice (ADR 0x052 §5): a speech model the account can
// use, one of the voices it offers, and a style. It is its own route so that a
// personality PUT, from an editor that knows nothing of voices, never clears one.
func (h *Handler) SetVoice(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	var req models.PersonalityVoice
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ModelID == uuid.Nil || req.Voice == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Send a model_id and a voice", err)
		return
	}
	h.respondVoice(w, r, userID, personalityID, &req)
}

// ClearVoice takes the personality's voice away: its replies are no longer read aloud.
func (h *Handler) ClearVoice(w http.ResponseWriter, r *http.Request) {
	userID, personalityID, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	h.respondVoice(w, r, userID, personalityID, nil)
}

func (h *Handler) voiceTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}

func (h *Handler) respondVoice(w http.ResponseWriter, r *http.Request, userID, personalityID uuid.UUID, voice *models.PersonalityVoice) {
	p, err := h.ds.SetPersonalityVoice(r.Context(), userID, personalityID, voice)
	switch {
	case errors.Is(err, datastore.ErrPersonalityNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", nil)
	case errors.Is(err, models.ErrInvalidPersonalityVoice):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to save the voice", err)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, p)
	}
}
