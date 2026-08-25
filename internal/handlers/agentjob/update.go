package agentjob

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type updateAgentJobRequest struct {
	Title         *string `json:"title,omitempty"`
	Prompt        *string `json:"prompt,omitempty"`
	ChatID        *string `json:"chat_id,omitempty"`
	PersonalityID *string `json:"personality_id,omitempty"`
	ModelID       *string `json:"model_id,omitempty"`
	ScheduleInput *string `json:"schedule_input,omitempty"`
	Timezone      *string `json:"timezone,omitempty"`
}

func (h *Handler) UpdateAgentJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok || idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "AgentJob ID is required", nil)
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid AgentJob ID", err)
		return
	}

	var req updateAgentJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Load current job (for default timezone).
	current, err := h.provider.GetAgentJob(r.Context(), userID, id)
	if err == datastore.ErrAgentJobNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
		return
	}
	if err != nil {
		h.logger.Error("failed to load agent job for update", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load agent job", err)
		return
	}

	updated := current

	// Update title (optional).
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		var titlePtr *string
		if title != "" {
			titlePtr = &title
		}
		updated, err = h.provider.UpdateAgentJobTitle(r.Context(), userID, id, titlePtr)
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err != nil {
			h.logger.Error("failed to update agent job title", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update agent job title", err)
			return
		}
	}

	// Update prompt (optional).
	if req.Prompt != nil {
		prompt := strings.TrimSpace(*req.Prompt)
		if prompt == "" {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "prompt cannot be empty", nil)
			return
		}

		updated, err = h.provider.UpdateAgentJobPrompt(r.Context(), userID, id, prompt)
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err != nil {
			h.logger.Error("failed to update agent job prompt", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update agent job prompt", err)
			return
		}
	}

	// Update associated chat (optional).
	// Empty string clears the chat association.
	if req.ChatID != nil {
		chatIDStr := strings.TrimSpace(*req.ChatID)
		var chatID *uuid.UUID
		if chatIDStr != "" {
			parsedChatID, parseErr := uuid.Parse(chatIDStr)
			if parseErr != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat_id", parseErr)
				return
			}
			chatID = &parsedChatID
		}

		updated, err = h.provider.SetAgentJobChat(r.Context(), userID, id, chatID)
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err == datastore.ErrChatNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
			return
		}
		if err != nil {
			h.logger.Error("failed to update agent job chat", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update agent job chat", err)
			return
		}
	}

	var patch models.SetAgentJobOverridesPatch
	if req.PersonalityID != nil {
		patch.UpdatePersonality = true
		trimmed := strings.TrimSpace(*req.PersonalityID)
		if trimmed != "" {
			parsedID, parseErr := uuid.Parse(trimmed)
			if parseErr != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id", parseErr)
				return
			}
			patch.PersonalityID = &parsedID
		}
	}
	if req.ModelID != nil {
		patch.UpdateModel = true
		trimmed := strings.TrimSpace(*req.ModelID)
		if trimmed != "" {
			parsedID, parseErr := uuid.Parse(trimmed)
			if parseErr != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid model_id", parseErr)
				return
			}
			patch.ModelID = &parsedID
		}
	}
	if patch.UpdatePersonality || patch.UpdateModel {
		updated, err = h.provider.SetAgentJobOverrides(r.Context(), userID, id, patch)
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err == datastore.ErrInvalidRequestBody {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id or model_id", nil)
			return
		}
		if err != nil {
			h.logger.Error("failed to update agent job overrides", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update agent job overrides", err)
			return
		}
	}

	// Update schedule (optional, but requires schedule_input).
	if req.ScheduleInput != nil {
		if h.scheduleParser == nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Schedule parser not configured", nil)
			return
		}

		scheduleInput := strings.TrimSpace(*req.ScheduleInput)
		if scheduleInput == "" {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "schedule_input cannot be empty", nil)
			return
		}

		// Default to the authenticated user's timezone (persisted on the User).
		// Fall back to the job's stored timezone for backwards compatibility.
		tz := ""
		if ctxTZ, ok := middleware.GetClientTimezoneFromContext(r.Context()); ok {
			tz = strings.TrimSpace(ctxTZ)
		}
		if tz == "" {
			tz = updated.Timezone
		}
		if req.Timezone != nil && strings.TrimSpace(*req.Timezone) != "" {
			tz = strings.TrimSpace(*req.Timezone)
		}
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid timezone (expected IANA name like America/New_York)", err)
				return
			}
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

		updated, err = h.provider.UpdateAgentJobSchedule(
			r.Context(),
			userID,
			id,
			scheduleInput,
			preview.ScheduleType,
			preview.Schedule,
			preview.RunAt,
			preview.Timezone,
			nextRunAt,
		)
		if err == datastore.ErrAgentJobNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "AgentJob not found", nil)
			return
		}
		if err != nil {
			h.logger.Error("failed to update agent job schedule", zap.String("user_id", userID.String()), zap.String("agent_job_id", id.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update agent job schedule", err)
			return
		}
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updated)
}
