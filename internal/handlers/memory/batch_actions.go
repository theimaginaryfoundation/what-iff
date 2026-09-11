package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type batchDeleteRequest struct {
	IDs       []string `json:"ids"`
	AllOrNone bool     `json:"all_or_none"`
}

type batchPatchRequest struct {
	IDs       []string       `json:"ids"`
	Patch     map[string]any `json:"patch"`
	AllOrNone bool           `json:"all_or_none"`
}

func parseBatchIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("ids is required")
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func parseBatchPatch(payload map[string]any) (models.MemoryPatch, error) {
	var patch models.MemoryPatch
	if payload == nil {
		return patch, fmt.Errorf("patch is required")
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return patch, err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return patch, err
	}
	if len(fields) == 0 {
		return patch, fmt.Errorf("patch must include at least one field")
	}

	if rawContent, exists := fields["content"]; exists {
		var content string
		if err := json.Unmarshal(rawContent, &content); err != nil {
			return patch, fmt.Errorf("invalid content")
		}
		patch.Content = &content
	}
	if rawLevel, exists := fields["level"]; exists {
		var level models.MemoryLevel
		if err := json.Unmarshal(rawLevel, &level); err != nil {
			return patch, fmt.Errorf("invalid level")
		}
		patch.Level = &level
	}
	if rawType, exists := fields["type"]; exists {
		var memoryType models.MemoryType
		if err := json.Unmarshal(rawType, &memoryType); err != nil {
			return patch, fmt.Errorf("invalid type")
		}
		patch.Type = &memoryType
	}
	if rawStarred, exists := fields["starred"]; exists {
		var starred bool
		if err := json.Unmarshal(rawStarred, &starred); err != nil {
			return patch, fmt.Errorf("invalid starred")
		}
		patch.Starred = &starred
	}
	if rawStatus, exists := fields["status"]; exists {
		var status models.MemoryStatus
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			return patch, fmt.Errorf("invalid status")
		}
		if status != models.MemoryStatusActive && status != models.MemoryStatusInactive {
			return patch, fmt.Errorf("status must be active or inactive")
		}
		patch.Status = &status
	}
	if rawConfidence, exists := fields["confidence"]; exists {
		var confidence models.MemoryConfidence
		if err := json.Unmarshal(rawConfidence, &confidence); err != nil {
			return patch, fmt.Errorf("invalid confidence")
		}
		if confidence != models.MemoryConfidenceLow &&
			confidence != models.MemoryConfidenceMedium &&
			confidence != models.MemoryConfidenceHigh {
			return patch, fmt.Errorf("confidence must be low, medium, or high")
		}
		patch.Confidence = &confidence
	}
	if rawChatID, exists := fields["chat_id"]; exists {
		patch.SetChatID = true
		if string(rawChatID) != "null" {
			var chatIDStr string
			if err := json.Unmarshal(rawChatID, &chatIDStr); err != nil {
				return patch, fmt.Errorf("invalid chat_id")
			}
			if chatIDStr != "" {
				chatID, err := uuid.Parse(chatIDStr)
				if err != nil {
					return patch, fmt.Errorf("invalid chat_id")
				}
				patch.ChatID = &chatID
			}
		}
	}
	if rawPinned, exists := fields["pinned_personality_id"]; exists {
		patch.SetPinnedPersonalityID = true
		if string(rawPinned) != "null" {
			var personalityIDStr string
			if err := json.Unmarshal(rawPinned, &personalityIDStr); err != nil {
				return patch, fmt.Errorf("invalid pinned_personality_id")
			}
			if personalityIDStr != "" {
				personalityID, err := uuid.Parse(personalityIDStr)
				if err != nil {
					return patch, fmt.Errorf("invalid pinned_personality_id")
				}
				patch.PinnedPersonalityID = &personalityID
			}
		}
	}

	return patch, nil
}

// DeleteMemoriesBatch deletes multiple memories for the authenticated user.
func (h *Handler) DeleteMemoriesBatch(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req batchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	ids, err := parseBatchIDs(req.IDs)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ids", err)
		return
	}

	result, err := h.ds.DeleteMemoriesBatch(r.Context(), userID, models.BatchDeleteMemoryInput{
		IDs:       ids,
		AllOrNone: req.AllOrNone,
	})
	if err != nil {
		if errors.Is(err, datastore.ErrMemoryNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Memory not found", err)
			return
		}
		h.logger.Error("failed to delete memory batch", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete memories", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, result)
}

// PatchMemoriesBatch applies the same patch to multiple memories for the authenticated user.
func (h *Handler) PatchMemoriesBatch(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req batchPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	ids, err := parseBatchIDs(req.IDs)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ids", err)
		return
	}

	patch, err := parseBatchPatch(req.Patch)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid patch", err)
		return
	}

	result, err := h.ds.PatchMemoriesBatch(r.Context(), userID, models.BatchPatchMemoryInput{
		IDs:       ids,
		Patch:     patch,
		AllOrNone: req.AllOrNone,
	})
	if err != nil {
		if errors.Is(err, datastore.ErrMemoryNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Memory not found", err)
			return
		}
		status, message := classifyMemoryWriteError(err)
		if status >= http.StatusInternalServerError {
			h.logger.Error("failed to patch memory batch", zap.String("user_id", userID.String()), zap.Error(err))
			message = "Failed to patch memories"
		}
		handlerutils.RespondWithError(w, h.logger, status, handlerutils.CodeNotSet, message, err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, result)
}
