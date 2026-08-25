package fileattachment

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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
	//TODO: Fix this to properly delete with the new file store + S3 integration.
	fileAttachment, err := h.ds.GetFileAttachment(r.Context(), userID, id)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", err)
		return
	}

	if fileAttachment.FileID != nil {
		err = h.agent.OpenAIProvider.DeleteFileAttachment(r.Context(), *fileAttachment.FileID)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting file attachment", err)
			return
		}
	}

	err = h.ds.DeleteFileAttachment(r.Context(), userID, id)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting file attachment", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, nil)
}
