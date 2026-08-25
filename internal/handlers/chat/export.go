package chat

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// ExportChat exports a chat and all its messages as a ZIP file
func (h *Handler) ExportChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	// Get chat first to retrieve its name and verify ownership
	chat, err := h.ds.GetChat(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get chat for export",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get chat", err)
		return
	}

	// Sanitize chat name for use in filename
	safeChatName := sanitizeFilename(chat.Name)
	filename := fmt.Sprintf("%s.zip", safeChatName)

	// Set headers for ZIP file download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-cache")

	// Stream the export directly to the response writer
	// If an error occurs during streaming, the ZIP will be finalized
	// but may be incomplete. HTTP status codes indicate success/partial success.
	if err := h.ds.ExportChat(r.Context(), userID, chatID, w); err != nil {
		// Log the error but don't try to write a response body
		// Headers have already been sent at this point
		h.logger.Error("failed to export chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		// Note: We cannot send an error response at this point since
		// we've already started writing the ZIP file to the response.
		// The client will receive a potentially incomplete but valid ZIP file.
		return
	}

	h.logger.Info("successfully exported chat",
		zap.String("user_id", userID.String()),
		zap.String("chat_id", chatID.String()),
		zap.String("filename", filename))
}

// sanitizeFilename removes or replaces characters that are unsafe for filenames
func sanitizeFilename(name string) string {
	// Replace common problematic characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
		"\n", " ",
		"\r", " ",
		"\t", " ",
	)

	sanitized := replacer.Replace(name)

	// Trim whitespace
	sanitized = strings.TrimSpace(sanitized)

	// Replace multiple spaces with a single space
	sanitized = strings.Join(strings.Fields(sanitized), " ")

	// Trim any leading/trailing dashes and spaces
	sanitized = strings.Trim(sanitized, "- ")

	// Check if we're left with only dashes or empty string
	onlyDashes := true
	for _, ch := range sanitized {
		if ch != '-' && ch != ' ' {
			onlyDashes = false
			break
		}
	}

	// If the name is empty or only dashes after sanitization, use a default
	if sanitized == "" || onlyDashes {
		sanitized = "chat-export"
	}

	// Limit length to avoid issues with filesystem limits
	if len(sanitized) > 200 {
		sanitized = sanitized[:200]
		// Trim any trailing incomplete words or spaces
		sanitized = strings.TrimSpace(sanitized)
	}

	return sanitized
}
