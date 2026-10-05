package fileattachment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// ListFileAttachments returns a paginated list of file attachments for the authenticated user
func (h *Handler) ListFileAttachments(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()

	// Parse pagination parameters
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	// Parse filter parameters
	name := queryParams.Get("name")
	fileType := queryParams.Get("file_type")
	chatMessageIDStr := queryParams.Get("chat_message_id")
	personalityIDStr := queryParams.Get("personality_id")
	docsOnlyStr := queryParams.Get("docs_only")
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")

	filters := models.FileAttachmentFilters{}

	if name != "" {
		filters.Name = &name
	}

	if fileType != "" {
		filters.FileType = &fileType
	}

	if chatMessageIDStr != "" {
		if chatMessageID, err := uuid.Parse(chatMessageIDStr); err == nil {
			filters.ChatMessageID = &chatMessageID
		}
	}

	if personalityIDStr != "" {
		if personalityID, err := uuid.Parse(personalityIDStr); err == nil {
			filters.PersonalityID = &personalityID
		}
	}

	if docsOnlyStr != "" {
		if v, err := strconv.ParseBool(docsOnlyStr); err == nil {
			filters.DocsOnly = &v
		} else {
			h.logger.Warn("invalid docs_only query param, treating as unset",
				zap.String("value", docsOnlyStr))
		}
	}

	if minDateStr != "" {
		minDate, err := time.Parse(time.RFC3339, minDateStr)
		if err == nil {
			filters.MinDate = &minDate
		}
	}

	if maxDateStr != "" {
		maxDate, err := time.Parse(time.RFC3339, maxDateStr)
		if err == nil {
			filters.MaxDate = &maxDate
		}
	}

	// Get paginated file attachments
	fileAttachmentsPage, err := h.ds.ListFileAttachments(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list file attachments",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list file attachments", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, fileAttachmentsPage)
}

func (h *Handler) DeleteFileAttachment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]

	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid attachment ID", err)
		return
	}
	fileAttachment, err := h.ds.GetFileAttachment(r.Context(), userID, id)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", err)
		return
	}

	// Reference copies clone the provider FileID; only the last row to go deletes the file.
	if fileAttachment.FileID != nil && *fileAttachment.FileID != "" {
		shared, err := h.ds.FileAttachmentProviderFileShared(r.Context(), id, *fileAttachment.FileID)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting file attachment", err)
			return
		}
		if !shared {
			if err := h.agent.DeleteProviderFileAttachment(r.Context(), *fileAttachment.FileID); err != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting file attachment", err)
				return
			}
		}
	}

	err = h.ds.DeleteFileAttachment(r.Context(), userID, id)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting file attachment", err)
		return
	}

	// The row is gone, so the stored object goes too unless a reference copy still points at it.
	// Best effort: the delete has happened; an object that cannot be removed is left orphaned.
	// WithoutCancel so a client hanging up does not abandon the cleanup halfway.
	storage.ReleaseAttachmentObjects(context.WithoutCancel(r.Context()), h.logger, h.agent.FileStore(), h.ds, userID,
		[]models.FileAttachment{*fileAttachment})

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, nil)
}

// GetFileAttachmentContent returns attachment bytes for the authenticated user.
func (h *Handler) GetFileAttachmentContent(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid attachment ID", err)
		return
	}

	attachment, err := h.ds.GetFileAttachment(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to fetch file attachment", err)
		}
		return
	}

	fileStore := h.agent.FileStore()
	if fileStore == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File storage not configured", nil)
		return
	}

	keys := make([]string, 0, 2)
	if k := strings.TrimSpace(attachment.S3Key); k != "" {
		keys = append(keys, k)
	}
	// Legacy rows without s3_key: derive the key. FileKeyForAttachment takes the CHAT id (the
	// datastore resolves it through the chat message), not the chat message id.
	keys = append(keys, storage.FileKeyForAttachment(
		userID,
		attachment.ID,
		attachment.Name,
		attachment.FileType,
		attachment.ChatID,
		attachment.PersonalityID,
	))

	var data []byte
	for _, key := range keys {
		blob, downloadErr := fileStore.DownloadFile(r.Context(), key)
		if downloadErr != nil {
			handlerutils.RespondWithError(
				w,
				h.logger,
				http.StatusInternalServerError,
				handlerutils.CodeNotSet,
				fmt.Sprintf("Failed to download file attachment %q", attachment.Name),
				downloadErr,
			)
			return
		}
		if len(blob) != 0 {
			data = blob
			break
		}
	}
	if len(data) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File content not available", nil)
		return
	}

	contentType := strings.TrimSpace(attachment.FileType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	filename := strings.TrimSpace(attachment.Name)
	if filename == "" {
		filename = attachment.ID.String()
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
