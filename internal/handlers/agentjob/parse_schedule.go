package agentjob

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"go.uber.org/zap"
)

type parseScheduleRequest struct {
	ScheduleInput string `json:"schedule_input"`
	Timezone      string `json:"timezone,omitempty"`
	Now           string `json:"now,omitempty"`
}

func (h *Handler) ParseSchedule(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	if h.scheduleParser == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Schedule parser not configured", nil)
		return
	}

	var req parseScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	req.ScheduleInput = strings.TrimSpace(req.ScheduleInput)
	if req.ScheduleInput == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "schedule_input is required", nil)
		return
	}

	now := time.Now()
	if strings.TrimSpace(req.Now) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Now))
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid now timestamp (expected RFC3339)", err)
			return
		}
		now = t
	}

	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		if ctxTZ, ok := middleware.GetClientTimezoneFromContext(r.Context()); ok {
			tz = strings.TrimSpace(ctxTZ)
		}
	}
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid timezone (expected IANA name like America/New_York)", err)
			return
		}
	}

	preview, err := h.scheduleParser.ParseAgentJobSchedule(r.Context(), userID, req.ScheduleInput, tz, now)
	if err != nil {
		h.logger.Debug("failed to parse schedule", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Failed to interpret schedule", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, preview)
}
