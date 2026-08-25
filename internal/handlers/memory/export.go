package memory

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"go.uber.org/zap"
)

// ExportMemories handles GET /memory/export.
// exports all of a user's memories as a ZIP file
func (h *Handler) ExportMemories(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	if allowed, retryAfter := h.exportLimiter.allow(userID); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, handlerutils.CodeNotSet, "Export rate limit exceeded; please wait before retrying", nil)
		return
	}

	filename := fmt.Sprintf("memories-%s.zip", time.Now().UTC().Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-cache")

	if err := h.ds.ExportMemories(r.Context(), userID, w); err != nil {
		h.logger.Error("failed to export memories",
			zap.String("user_id", userID.String()),
			zap.String("note", "headers already sent; response may be partial"),
			zap.Error(err))
		return
	}

	h.logger.Info("memories exported",
		zap.String("user_id", userID.String()),
		zap.String("filename", filename))
}
