package memory

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

func (h *Handler) PatchMemory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	memoryIDStr, ok := vars["id"]
	if !ok || memoryIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Memory ID is required", nil)
		return
	}

	memoryID, err := uuid.Parse(memoryIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid memory ID", err)
		return
	}

	var payload map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	var patch models.MemoryPatch
	if raw, exists := payload["content"]; exists {
		var content string
		if err := json.Unmarshal(raw, &content); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid content", err)
			return
		}
		patch.Content = &content
	}
	if raw, exists := payload["level"]; exists {
		var level models.MemoryLevel
		if err := json.Unmarshal(raw, &level); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid level", err)
			return
		}
		patch.Level = &level
	}
	if raw, exists := payload["type"]; exists {
		var memoryType models.MemoryType
		if err := json.Unmarshal(raw, &memoryType); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid type", err)
			return
		}
		patch.Type = &memoryType
	}
	if raw, exists := payload["starred"]; exists {
		var starred bool
		if err := json.Unmarshal(raw, &starred); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid starred value", err)
			return
		}
		patch.Starred = &starred
	}
	if raw, exists := payload["status"]; exists {
		var status models.MemoryStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid status", err)
			return
		}
		if status != models.MemoryStatusActive && status != models.MemoryStatusInactive {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid status", fmt.Errorf("status must be active or inactive"))
			return
		}
		patch.Status = &status
	}
	if raw, exists := payload["confidence"]; exists {
		var confidence models.MemoryConfidence
		if err := json.Unmarshal(raw, &confidence); err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid confidence", err)
			return
		}
		if confidence != models.MemoryConfidenceLow &&
			confidence != models.MemoryConfidenceMedium &&
			confidence != models.MemoryConfidenceHigh {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid confidence", fmt.Errorf("confidence must be low, medium, or high"))
			return
		}
		patch.Confidence = &confidence
	}
	if raw, exists := payload["chat_id"]; exists {
		patch.SetChatID = true
		if string(raw) != "null" {
			var chatIDStr string
			if err := json.Unmarshal(raw, &chatIDStr); err != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat_id", err)
				return
			}
			if chatIDStr != "" {
				chatID, err := uuid.Parse(chatIDStr)
				if err != nil {
					handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat_id", err)
					return
				}
				patch.ChatID = &chatID
			}
		}
	}
	if raw, exists := payload["pinned_personality_id"]; exists {
		patch.SetPinnedPersonalityID = true
		if string(raw) != "null" {
			var personalityIDStr string
			if err := json.Unmarshal(raw, &personalityIDStr); err != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid pinned_personality_id", err)
				return
			}
			if personalityIDStr != "" {
				personalityID, err := uuid.Parse(personalityIDStr)
				if err != nil {
					handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid pinned_personality_id", err)
					return
				}
				patch.PinnedPersonalityID = &personalityID
			}
		}
	}

	updated, err := h.ds.UpdateMemory(r.Context(), userID, memoryID, patch)
	if err == datastore.ErrMemoryNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Memory not found", err)
		return
	}
	if err == datastore.ErrChatNotFound || err == datastore.ErrPersonalityNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, err.Error(), err)
		return
	}
	if err != nil {
		h.logger.Error("failed to patch memory", zap.String("user_id", userID.String()), zap.String("memory_id", memoryID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Failed to patch memory", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updated)
}
