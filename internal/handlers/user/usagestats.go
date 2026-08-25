package user

import (
	"net/http"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

func (h *Handler) GetUsageStats(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()
	period := queryParams.Get("period")

	endDate := time.Now()
	var startDate time.Time

	switch period {
	case "day":
		startDate = endDate.AddDate(0, 0, -1)
	case "week":
		startDate = endDate.AddDate(0, 0, -7)
	case "month":
		startDate = endDate.AddDate(0, -1, 0)
	default:
		startDate = endDate.AddDate(0, 0, -30)
	}

	// Get usage stats using datastore
	usageStats, err := h.store.GetUsageStats(r.Context(), startDate, endDate, userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error fetching usage stats", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, usageStats)
}
