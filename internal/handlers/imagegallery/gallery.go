package imagegallery

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ListImages returns a paginated list of image attachments for the authenticated user.
// file_content is deliberately excluded from the list response; clients fetch bytes
// via GetImageContent.
func (h *Handler) ListImages(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	page := handlerutils.ParseIntParam(r.URL.Query().Get("page"), 1)
	limit := handlerutils.ParseIntParam(r.URL.Query().Get("limit"), 20)

	imageType := models.ImageMIMEPrefix
	filters := models.FileAttachmentFilters{FileType: &imageType}

	// Optional filename search. Trimmed to avoid whitespace-only filters silently
	// returning the full library. Used both by the gallery view's search box and by
	// the cross-resource /search endpoint when it composes against this handler.
	if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
		filters.Name = &name
	}
	if personalityIDRaw := strings.TrimSpace(r.URL.Query().Get("personality_id")); personalityIDRaw != "" {
		personalityID, parseErr := uuid.Parse(personalityIDRaw)
		if parseErr != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id", parseErr)
			return
		}
		filters.PersonalityID = &personalityID
	}
	if globalOnlyRaw := strings.TrimSpace(r.URL.Query().Get("global_only")); globalOnlyRaw != "" {
		globalOnly, parseErr := strconv.ParseBool(globalOnlyRaw)
		if parseErr != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid global_only flag", parseErr)
			return
		}
		filters.GlobalOnly = &globalOnly
		if globalOnly {
			filters.PersonalityID = nil
		}
	}

	result, err := h.ds.ListFileAttachments(r.Context(), userID, page, limit, filters)
	if err != nil {
		h.logger.Error("image gallery: failed to list images",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list images", err)
		return
	}

	// Gallery rows can include lightweight reference attachments created for chat
	// reuse. Dedupe by underlying object identity so UI shows a single asset card.
	result.Results = dedupeGalleryImages(result.Results)
	// Keep TotalCount from ListFileAttachments (pre-page DB count). Replacing it with
	// len(deduped page) broke pagination so older pages were unreachable.

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, result)
}

func dedupeGalleryImages(rows []any) []any {
	if len(rows) <= 1 {
		return rows
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		att, ok := row.(*models.FileAttachment)
		if !ok || att == nil {
			out = append(out, row)
			continue
		}

		key := strings.TrimSpace(att.S3Key)
		if key == "" && att.FileID != nil {
			key = "file_id:" + strings.TrimSpace(*att.FileID)
		}
		if key == "" {
			// Keep records with no shared identity key.
			out = append(out, row)
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

// GetImageContent proxies image bytes for a single attachment. Query param:
//
//	?size=thumbnail  — returns the JPEG thumbnail (default)
//	?size=full       — returns the full-resolution image
//
// Resolution order: canonical and derived S3/local object keys.
func (h *Handler) GetImageContent(w http.ResponseWriter, r *http.Request) {
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
			h.logger.Error("image gallery: failed to fetch attachment",
				zap.String("id", idStr),
				zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to fetch image", err)
		}
		return
	}

	if !strings.HasPrefix(attachment.FileType, models.ImageMIMEPrefix) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Not an image attachment", nil)
		return
	}

	size := r.URL.Query().Get("size")
	wantThumb := size != "full"

	data, contentType := h.resolveImageBytes(r, userID, attachment, wantThumb)
	if len(data) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Image data not available", nil)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// resolveImageBytes fetches bytes for an attachment from canonical and derived object keys.
func (h *Handler) resolveImageBytes(r *http.Request, userID uuid.UUID, att *models.FileAttachment, wantThumb bool) ([]byte, string) {
	return handlerutils.ResolveImageBytes(r.Context(), h.logger, h.fileStore, userID, att, wantThumb)
}
