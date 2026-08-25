package agentjob

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const maxCreateAgentJobBodyBytes = 64 << 10 // 64 KiB; prompt + schedule JSON payload

type createAgentJobRequest struct {
	Title         *string `json:"title,omitempty"`
	Prompt        string  `json:"prompt"`
	ScheduleInput string  `json:"schedule_input"`
	Timezone      *string `json:"timezone,omitempty"`
	ChatID        *string `json:"chat_id,omitempty"`
	PersonalityID *string `json:"personality_id,omitempty"`
	ModelID       *string `json:"model_id,omitempty"`
}

func (h *Handler) CreateAgentJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	if h.scheduleParser == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Schedule parser not configured", nil)
		return
	}

	var req createAgentJobRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxCreateAgentJobBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	scheduleInput := strings.TrimSpace(req.ScheduleInput)
	if scheduleInput == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "schedule_input is required", nil)
		return
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "prompt is required", nil)
		return
	}

	tz := ""
	if ctxTZ, ok := middleware.GetClientTimezoneFromContext(r.Context()); ok {
		tz = strings.TrimSpace(ctxTZ)
	}
	if req.Timezone != nil && strings.TrimSpace(*req.Timezone) != "" {
		tz = strings.TrimSpace(*req.Timezone)
	}
	if tz == "" {
		tz = "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid timezone (expected IANA name like America/New_York)", err)
		return
	}

	preview, err := h.scheduleParser.ParseAgentJobSchedule(r.Context(), userID, scheduleInput, tz, time.Now())
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Failed to interpret schedule", err)
		return
	}

	var nextRunAt *time.Time
	if len(preview.NextRuns) > 0 {
		n := preview.NextRuns[0]
		nextRunAt = &n
	}

	var titlePtr *string
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title != "" {
			titlePtr = &title
		}
	}

	newJob := models.AgentJob{
		Title:         titlePtr,
		Prompt:        prompt,
		ScheduleInput: scheduleInput,
		ScheduleType:  preview.ScheduleType,
		Schedule:      preview.Schedule,
		RunAt:         preview.RunAt,
		Timezone:      preview.Timezone,
		Status:        models.AgentJobStatusActive,
		NextRunAt:     nextRunAt,
		RunCount:      0,
	}

	if req.ChatID != nil {
		chatIDStr := strings.TrimSpace(*req.ChatID)
		if chatIDStr != "" {
			parsedChatID, parseErr := uuid.Parse(chatIDStr)
			if parseErr != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat_id", parseErr)
				return
			}
			newJob.ChatID = &parsedChatID
		}
	}
	if req.PersonalityID != nil {
		trimmed := strings.TrimSpace(*req.PersonalityID)
		if trimmed != "" {
			parsedID, parseErr := uuid.Parse(trimmed)
			if parseErr != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id", parseErr)
				return
			}
			newJob.PersonalityID = &parsedID
		}
	}
	if req.ModelID != nil {
		trimmed := strings.TrimSpace(*req.ModelID)
		if trimmed != "" {
			parsedID, parseErr := uuid.Parse(trimmed)
			if parseErr != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid model_id", parseErr)
				return
			}
			newJob.ModelID = &parsedID
		}
	}

	created, err := h.provider.CreateAgentJob(r.Context(), userID, newJob)
	if errors.Is(err, datastore.ErrChatNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
		return
	}
	if errors.Is(err, datastore.ErrInvalidRequestBody) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id or model_id", nil)
		return
	}
	if err != nil {
		h.logger.Error("failed to create agent job", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create agent job", nil)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, created)
}
