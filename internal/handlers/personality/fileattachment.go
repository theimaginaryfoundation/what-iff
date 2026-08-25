package personality

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
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
	idStr := vars["id"]

	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}

	if id == uuid.Nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", errors.New("personality ID is required"))
		return
	}

	fileAttachment, tempFilePath, err := handlerutils.UploadFileAttachment(w, r, h.logger, h.agent.OpenAIProvider, userID, map[string]string{"personality_id": id.String()})
	if err != nil {
		return
	}
	originalName := fileAttachment.Name
	fileAttachment.Name = normalizeAttachmentName(originalName, strings.TrimSpace(r.FormValue("title")))
	if description := strings.TrimSpace(r.FormValue("description")); description != "" {
		fileAttachment.Description = &description
	}

	fileAttachment.PersonalityID = &id

	createdAttachment, err := h.ds.CreateFileAttachment(r.Context(), userID, fileAttachment)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error creating file attachment", err)
		return
	}

	s3Key := storage.FileKeyForPersonality(userID, id, createdAttachment.ID, fileAttachment.Name)
	if err := handlerutils.UploadToS3(r.Context(), h.agent.FileStore(), s3Key, tempFilePath, fileAttachment.FileType); err != nil {
		h.logger.Error("S3 upload failed, rolling back file attachment record",
			zap.Error(err),
			zap.String("file_attachment_id", createdAttachment.ID.String()))
		_ = h.ds.DeleteFileAttachment(r.Context(), userID, createdAttachment.ID)
		_ = os.Remove(tempFilePath)
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error saving file — please retry", err)
		return
	}
	if err := h.ds.SetFileAttachmentS3Key(r.Context(), userID, createdAttachment.ID, s3Key); err != nil {
		h.logger.Warn("failed to persist personality attachment s3_key",
			zap.String("file_attachment_id", createdAttachment.ID.String()),
			zap.String("s3_key", s3Key),
			zap.Error(err))
	}

	handlerutils.TriggerAsyncFileChunking(
		h.logger,
		h.agent.ChunkPipeline(),
		createdAttachment.ID,
		tempFilePath,
		fileAttachment.Name,
	)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, createdAttachment)
}

func normalizeAttachmentName(originalName, providedTitle string) string {
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
