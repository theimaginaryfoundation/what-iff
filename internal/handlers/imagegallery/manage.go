package imagegallery

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filechange"
	"github.com/theimaginaryfoundation/what-iff/internal/filedelete"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// DeleteImage deletes an image attachment: the stored full-size and thumbnail objects, then the
// row (filedelete.Service.DeleteImage). Other files are refused; they go through
// DELETE /file-attachment/{id} or the bulk POST /image-gallery/delete.
func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid image ID", err)
		return
	}

	err = h.deleter().DeleteImage(r.Context(), userID, id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, filedelete.ErrNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Image not found", nil)
	case errors.Is(err, filedelete.ErrNotImage):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Not an image attachment", nil)
	case filedelete.StageOf(err) == filedelete.StageLookup:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to fetch image", err)
	case filedelete.StageOf(err) == filedelete.StageStorage:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete image from storage", err)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete image", err)
	}
}

// renameRequest is the payload for PATCH /image-gallery/{id}.
type renameRequest struct {
	Name string `json:"name"`
}

// RenameImage updates the display name of an image attachment. The S3 key is
// left unchanged (display name is decoupled from storage path via the s3_key column).
func (h *Handler) RenameImage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid image ID", err)
		return
	}

	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Name must not be empty", nil)
		return
	}

	// The name before the rename, for the change event; a miss is reported by the rename itself.
	before, _ := h.ds.GetFileAttachment(r.Context(), userID, id)

	updated, err := h.ds.UpdateFileAttachmentName(r.Context(), userID, id, req.Name)
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrFileAttachmentNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File not found", nil)
		case errors.Is(err, datastore.ErrFileAttachmentPathTaken):
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "A file with that name is already in this folder", nil)
		case errors.Is(err, datastore.ErrSkillFileImmutable):
			handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Skill bundle files are renamed in the skill editor", nil)
		case errors.Is(err, models.ErrInvalidFileName):
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to rename file", err)
		}
		return
	}
	if before != nil && before.Name != updated.Name {
		filechange.Notify(r.Context(), filechange.Event{Kind: filechange.KindRename, UserID: userID, File: updated, OldName: before.Name})
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updated)
}

// ReferenceImage creates a lightweight file_attachment record referencing the
// same S3 object as the source attachment. Used by the chat UI to attach an
// existing gallery image to a new message without copying the S3 object.
func (h *Handler) ReferenceImage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	idStr := mux.Vars(r)["id"]
	srcID, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid image ID", err)
		return
	}

	ref, err := h.ds.CreateFileAttachmentReference(r.Context(), userID, srcID)
	if err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Image not found", nil)
		} else {
			h.logger.Error("image gallery: failed to create reference", zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to reference image", err)
		}
		return
	}
	// Thumbnails are stored per attachment id, so the copy needs its own.
	imageutil.CopyThumbnail(h.fileStore, h.logger, userID, srcID, ref.ID)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, ref)
}
