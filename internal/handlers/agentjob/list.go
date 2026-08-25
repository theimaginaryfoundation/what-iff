package agentjob

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"go.uber.org/zap"
)

func (h *Handler) ListAgentJobs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	filters := models.AgentJobFilters{}

	if statusStr := queryParams.Get("status"); statusStr != "" {
		status := models.AgentJobStatus(statusStr)
		filters.Status = &status
	}
	if stStr := queryParams.Get("schedule_type"); stStr != "" {
		st := models.AgentJobScheduleType(stStr)
		filters.ScheduleType = &st
	}
	if q := queryParams.Get("search"); q != "" {
		filters.Query = &q
	}

	resp, err := h.provider.ListAgentJobs(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list agent jobs", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list agent jobs", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, resp)
}
