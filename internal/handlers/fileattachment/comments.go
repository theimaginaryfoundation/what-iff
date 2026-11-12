package fileattachment

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Comments on text, code and image files (ADR 0x045), which have no place for them of their own.

type putFileCommentsRequest struct {
	Threads        []models.FileCommentThread `json:"threads"`
	ContentVersion int                        `json:"content_version"`
	Revision       int                        `json:"revision"`
}

func (h *Handler) comments() *filecomments.Service {
	return filecomments.New(h.ds, h.agent.FileStore())
}

func (h *Handler) commentsRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid attachment ID", err)
		return uuid.Nil, uuid.Nil, false
	}
	if h.agent.FileStore() == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "File storage not configured", nil)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}

// GetFileComments handles GET /file-attachment/{id}/comments: the file's comment threads, placed
// on its current content (carried across any change made since they were written).
func (h *Handler) GetFileComments(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.commentsRequest(w, r)
	if !ok {
		return
	}
	got, err := h.comments().Read(r.Context(), userID, id)
	if err != nil {
		h.respondCommentsError(w, err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, got)
}

// PutFileComments handles PUT /file-attachment/{id}/comments: the file's threads, placed on the
// content_version given, which must be its current content (412 otherwise), over the revision the
// client read (409 otherwise; 0 for a file with no comments yet).
func (h *Handler) PutFileComments(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.commentsRequest(w, r)
	if !ok {
		return
	}
	var req putFileCommentsRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContentVersion < 1 || req.Revision < 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid comments", err)
		return
	}
	got, err := h.comments().Write(r.Context(), userID, id, req.Threads, req.ContentVersion, req.Revision)
	if err != nil {
		h.respondCommentsError(w, err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, got)
}

func (h *Handler) respondCommentsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, datastore.ErrFileAttachmentNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", nil)
	case errors.Is(err, filecomments.ErrStaleContent):
		handlerutils.RespondWithError(w, h.logger, http.StatusPreconditionFailed, handlerutils.CodeNotSet, "The file changed since the comments were placed on it", nil)
	case errors.Is(err, filecomments.ErrRevision):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "The comments changed since they were read", nil)
	case errors.Is(err, filecomments.ErrNotText):
		handlerutils.RespondWithError(w, h.logger, http.StatusUnsupportedMediaType, handlerutils.CodeNotSet, "Comments can be placed only on images and on UTF-8 text files of at most 4 MB", nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read or write the comments", err)
	}
}
