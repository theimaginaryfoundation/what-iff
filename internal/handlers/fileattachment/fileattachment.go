package fileattachment

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filedelete"
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

// DeleteFileAttachment deletes any of the caller's attachments: the row, then the stored objects
// nothing else references (filedelete.Service.DeleteFile).
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
	err = filedelete.New(h.ds, h.agent.FileStore(), h.logger).DeleteFile(r.Context(), userID, id)
	switch {
	case err == nil:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, nil)
	case errors.Is(err, filedelete.ErrNotFound), filedelete.StageOf(err) == filedelete.StageLookup:
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", err)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting file attachment", err)
	}
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

	// A save replaces a file's bytes under the same URL, so a cached copy must be revalidated
	// (ADR 0x043 §3); content_version says whether it still holds.
	etag := strconv.Quote(strconv.Itoa(attachment.ContentVersion))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	fileStore := h.agent.FileStore()
	if fileStore == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File storage not configured", nil)
		return
	}

	data, downloadErr := storage.ReadAttachment(r.Context(), fileStore, storage.AttachmentObject{
		UserID: userID, ID: attachment.ID, S3Key: attachment.S3Key, Name: attachment.Name, FileType: attachment.FileType,
		ChatID: attachment.ChatID, PersonalityID: attachment.PersonalityID,
	})
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
