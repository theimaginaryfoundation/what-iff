package imagegallery

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// DeleteImage deletes an image attachment: removes the S3 full and thumbnail
// objects and then deletes the DB record.
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

	attachment, err := h.ds.GetFileAttachment(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Image not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to fetch image", err)
		}
		return
	}

	if !strings.HasPrefix(attachment.FileType, models.ImageMIMEPrefix) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Not an image attachment", nil)
		return
	}

	// Delete full-resolution object first. If this fails (non-NoSuchKey), fail fast
	// so DB and storage do not drift.
	if h.fileStore != nil {
		// Full-resolution object (use stored key when available, fall back to derived).
		fullKey := attachment.S3Key
		if fullKey == "" {
			fullKey = storage.FileKeyForImage(userID, attachment.ID, attachment.Name)
		}
		if err := h.fileStore.DeleteFile(r.Context(), fullKey); err != nil {
			h.logger.Error("image gallery: failed to delete S3 full image",
				zap.String("key", fullKey), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete image from storage", err)
			return
		}

		// Thumbnail delete is best-effort; keep warning-only behavior.
		thumbKey := storage.FileKeyForImageThumbnail(userID, attachment.ID)
		if err := h.fileStore.DeleteFile(r.Context(), thumbKey); err != nil {
			h.logger.Warn("image gallery: failed to delete S3 thumbnail",
				zap.String("key", thumbKey), zap.Error(err))
		}
	}

	if err := h.ds.DeleteFileAttachment(r.Context(), userID, id); err != nil {
		h.logger.Error("image gallery: failed to delete DB record",
			zap.String("id", idStr), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete image", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	updated, err := h.ds.UpdateFileAttachmentName(r.Context(), userID, id, req.Name)
	if err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Image not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to rename image", err)
		}
		return
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

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, ref)
}
