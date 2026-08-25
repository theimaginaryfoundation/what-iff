package imagegallery

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// ImportImage uploads a user image into the gallery without pinning it to a
// personality. Optional title/description metadata is persisted on the
// attachment record.
func (h *Handler) ImportImage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if h.agent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Image import is unavailable", nil)
		return
	}

	fileAttachment, tempFilePath, err := handlerutils.UploadFileAttachment(w, r, h.logger, h.agent.OpenAIProvider, userID, nil)
	if err != nil {
		return
	}
	originalName := fileAttachment.Name
	fileAttachment.Name = normalizeImportedName(originalName, strings.TrimSpace(r.FormValue("title")))
	if description := strings.TrimSpace(r.FormValue("description")); description != "" {
		fileAttachment.Description = &description
	}

	createdAttachment, err := h.ds.CreateFileAttachment(r.Context(), userID, fileAttachment)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error creating image", err)
		return
	}

	s3Key := storage.FileKeyForImage(userID, createdAttachment.ID, fileAttachment.Name)
	if err := handlerutils.UploadToS3(r.Context(), h.fileStore, s3Key, tempFilePath, fileAttachment.FileType); err != nil {
		h.logger.Error("image gallery import: S3 upload failed, rolling back record",
			zap.Error(err),
			zap.String("file_attachment_id", createdAttachment.ID.String()))
		_ = h.ds.DeleteFileAttachment(r.Context(), userID, createdAttachment.ID)
		_ = os.Remove(tempFilePath)
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error saving image — please retry", err)
		return
	}
	if err := h.ds.SetFileAttachmentS3Key(r.Context(), userID, createdAttachment.ID, s3Key); err != nil {
		h.logger.Warn("image gallery import: failed to persist s3 key",
			zap.String("file_attachment_id", createdAttachment.ID.String()),
			zap.String("s3_key", s3Key),
			zap.Error(err))
	}

	handlerutils.UploadThumbnailFromPath(r.Context(), h.fileStore, h.logger, userID, createdAttachment.ID, tempFilePath)
	handlerutils.TriggerAsyncFileChunking(
		h.logger,
		h.agent.ChunkPipeline(),
		createdAttachment.ID,
		tempFilePath,
		fileAttachment.Name,
	)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, createdAttachment)
}

func normalizeImportedName(originalName, providedTitle string) string {
	if providedTitle == "" {
		return originalName
	}
	title := strings.TrimSpace(filepath.Base(providedTitle))
	if title == "" {
		return originalName
	}
	originalExt := filepath.Ext(originalName)
	if filepath.Ext(title) == "" && originalExt != "" {
		return title + originalExt
	}
	return title
}
