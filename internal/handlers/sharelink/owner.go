package sharelink

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

// ListShareLinks handles GET /share-links: every link of the user's, newest first.
func (h *Handler) ListShareLinks(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	links, err := h.ds.ListShareLinks(r.Context(), userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list share links", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]any{"links": links})
}

// GetShareLink handles GET /share-links/files/{fileId}: the file's link, 404 when it has none.
func (h *Handler) GetShareLink(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.ownerRequest(w, r)
	if !ok {
		return
	}
	link, err := h.ds.ShareLinkForFile(r.Context(), userID, fileID)
	h.respondLink(w, link, http.StatusOK, err)
}

// PutShareLink handles PUT /share-links/files/{fileId}: shares the file (with a passcode unless
// the body says passcode_required: false); a file shared already keeps its link.
func (h *Handler) PutShareLink(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.ownerRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		PasscodeRequired *bool `json:"passcode_required"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", nil)
			return
		}
	}
	withPasscode := body.PasscodeRequired == nil || *body.PasscodeRequired
	link, created, err := h.ds.CreateShareLink(r.Context(), userID, fileID, withPasscode)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	h.respondLink(w, link, status, err)
}

// PatchShareLink handles PATCH /share-links/files/{fileId}: passcode_required turns the passcode
// on (a new one) or off; new_passcode makes a new one. A new passcode signs every visitor out.
func (h *Handler) PatchShareLink(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.ownerRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		PasscodeRequired *bool `json:"passcode_required"`
		NewPasscode      bool  `json:"new_passcode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", nil)
		return
	}
	link, err := h.ds.UpdateShareLink(r.Context(), userID, fileID, body.PasscodeRequired, body.NewPasscode)
	h.respondLink(w, link, http.StatusOK, err)
}

// DeleteShareLink handles DELETE /share-links/files/{fileId}: stops sharing the file; its link
// stops working and sharing again makes a new one.
func (h *Handler) DeleteShareLink(w http.ResponseWriter, r *http.Request) {
	userID, fileID, ok := h.ownerRequest(w, r)
	if !ok {
		return
	}
	if err := h.ds.DeleteShareLink(r.Context(), userID, fileID); err != nil {
		h.respondLink(w, nil, 0, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ownerRequest(w http.ResponseWriter, r *http.Request) (userID, fileID uuid.UUID, ok bool) {
	userID, ok = middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, uuid.Nil, false
	}
	fileID, err := uuid.Parse(mux.Vars(r)["fileId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid file ID", nil)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, fileID, true
}

func (h *Handler) respondLink(w http.ResponseWriter, link *models.ShareLink, status int, err error) {
	switch {
	case err == nil:
		handlerutils.RespondWithJSON(w, h.logger, status, link)
	case errors.Is(err, datastore.ErrFileAttachmentNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File not found", nil)
	case errors.Is(err, models.ErrShareLinkNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "The file is not shared", nil)
	case errors.Is(err, models.ErrShareNotShareable):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
	case errors.Is(err, datastore.ErrSharePasscodeUnavailable):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, err.Error(), nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update the share link", err)
	}
}
