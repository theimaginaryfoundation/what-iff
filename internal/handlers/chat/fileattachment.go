package chat

import (
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

func (h *Handler) CreateFileAttachment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	chatIDStr := vars["chatId"]

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	fileAttachment, tempFilePath, err := handlerutils.UploadFileAttachment(w, r, h.logger, h.agent.OpenAIProvider, userID, map[string]string{"chat_id": chatID.String()})
	if err != nil {
		return
	}

	createdAttachment, err := h.ds.CreateFileAttachment(r.Context(), userID, fileAttachment)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error creating file attachment", err)
		return
	}

	// Storage routing
	//
	// OpenAI vision does NOT read from our S3 bucket. The FileID returned by
	// UploadFileAttachment (above) is an OpenAI Files API identifier; OpenAI resolves
	// it internally from its own storage when the model runs. Our S3 paths are only
	// used by Claude (raw bytes download) and the image gallery.
	//
	// Given that, images and non-images are routed differently:
	//   • Images  → canonical images/ path (FileKeyForImage + thumbnail).
	//               Claude downloads from here; gallery serves from here.
	//   • Others  → chat-scoped path (FileKeyForChat).
	//               Used by the pgvector file-chunking pipeline.
	//
	// Both paths roll back the DB record on primary upload failure.
	if strings.HasPrefix(fileAttachment.FileType, models.ImageMIMEPrefix) {
		imgKey := storage.FileKeyForImage(userID, createdAttachment.ID, fileAttachment.Name)
		if err := handlerutils.UploadToS3(r.Context(), h.agent.FileStore(), imgKey, tempFilePath, fileAttachment.FileType); err != nil {
			h.logger.Error("S3 image upload failed, rolling back file attachment record",
				zap.Error(err),
				zap.String("file_attachment_id", createdAttachment.ID.String()))
			_ = h.ds.DeleteFileAttachment(r.Context(), userID, createdAttachment.ID)
			_ = os.Remove(tempFilePath)
			h.agent.RecordFileUpload(r.Context(), fileAttachment.FileType, "failure")
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error saving file — please retry", err)
			return
		}
		// Persist the S3 key so rename/delete can use it without re-deriving it.
		if err := h.ds.SetFileAttachmentS3Key(r.Context(), userID, createdAttachment.ID, imgKey); err != nil {
			h.logger.Warn("failed to persist image attachment s3_key",
				zap.String("file_attachment_id", createdAttachment.ID.String()),
				zap.String("s3_key", imgKey),
				zap.Error(err))
		}
		// Thumbnail is best-effort — failure never blocks the upload response.
		handlerutils.UploadThumbnailFromPath(r.Context(), h.agent.FileStore(), h.logger, userID, createdAttachment.ID, tempFilePath)
	} else {
		s3Key := storage.FileKeyForChat(userID, chatID, createdAttachment.ID, fileAttachment.Name)
		if err := handlerutils.UploadToS3(r.Context(), h.agent.FileStore(), s3Key, tempFilePath, fileAttachment.FileType); err != nil {
			h.logger.Error("S3 upload failed, rolling back file attachment record",
				zap.Error(err),
				zap.String("file_attachment_id", createdAttachment.ID.String()))
			_ = h.ds.DeleteFileAttachment(r.Context(), userID, createdAttachment.ID)
			_ = os.Remove(tempFilePath)
			h.agent.RecordFileUpload(r.Context(), fileAttachment.FileType, "failure")
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error saving file — please retry", err)
			return
		}
		if err := h.ds.SetFileAttachmentS3Key(r.Context(), userID, createdAttachment.ID, s3Key); err != nil {
			h.logger.Warn("failed to persist chat attachment s3_key",
				zap.String("file_attachment_id", createdAttachment.ID.String()),
				zap.String("s3_key", s3Key),
				zap.Error(err))
		}
	}
	h.agent.RecordFileUpload(r.Context(), fileAttachment.FileType, "success")

	handlerutils.TriggerAsyncFileChunking(
		h.logger,
		h.agent.ChunkPipeline(),
		createdAttachment.ID,
		tempFilePath,
		fileAttachment.Name,
	)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, createdAttachment)
}
