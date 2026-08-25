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

type createMemoryRequest struct {
	Content             string                  `json:"content"`
	Level               models.MemoryLevel      `json:"level"`
	ChatID              *string                 `json:"chat_id"`
	PinnedPersonalityID *string                 `json:"pinned_personality_id"`
	Type                models.MemoryType       `json:"type"`
	Starred             bool                    `json:"starred"`
	Confidence          models.MemoryConfidence `json:"confidence"`
}

type createMemoriesBatchRequest struct {
	Items     []createMemoryRequest `json:"items"`
	AllOrNone bool                  `json:"all_or_none"`
}

type createMemoriesBatchResponse struct {
	Results      []*models.Memory `json:"results"`
	CreatedCount int              `json:"created_count"`
}

func classifyMemoryWriteError(err error) (int, string) {
	switch {
	case errors.Is(err, datastore.ErrChatNotFound), errors.Is(err, datastore.ErrPersonalityNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, datastore.ErrInvalidRequestBody):
		return http.StatusBadRequest, "Invalid memory payload"
	default:
		return http.StatusInternalServerError, "Failed to persist memory"
	}
}

func parseOptionalUUID(value *string) (*uuid.UUID, error) {
	if value == nil || *value == "" {
		return nil, nil
	}

	id, err := uuid.Parse(*value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func isValidMemoryConfidence(conf models.MemoryConfidence) bool {
	return conf == "" ||
		conf == models.MemoryConfidenceLow ||
		conf == models.MemoryConfidenceMedium ||
		conf == models.MemoryConfidenceHigh
}

func toCreateMemoryInput(req createMemoryRequest) (models.CreateMemoryInput, error) {
	chatID, err := parseOptionalUUID(req.ChatID)
	if err != nil {
		return models.CreateMemoryInput{}, err
	}

	pinnedID, err := parseOptionalUUID(req.PinnedPersonalityID)
	if err != nil {
		return models.CreateMemoryInput{}, err
	}

	if !isValidMemoryConfidence(req.Confidence) {
		return models.CreateMemoryInput{}, fmt.Errorf("invalid confidence")
	}

	return models.CreateMemoryInput{
		Content:             req.Content,
		Level:               req.Level,
		ChatID:              chatID,
		PinnedPersonalityID: pinnedID,
		Type:                req.Type,
		Starred:             req.Starred,
		Confidence:          req.Confidence,
	}, nil
}

func (h *Handler) CreateMemory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req createMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	input, err := toCreateMemoryInput(req)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid UUID in request", err)
		return
	}

	mem, err := h.ds.CreateMemoryFromInput(r.Context(), userID, input)
	if err != nil {
		h.logger.Error("failed to create memory", zap.String("user_id", userID.String()), zap.Error(err))
		status, msg := classifyMemoryWriteError(err)
		handlerutils.RespondWithError(w, h.logger, status, handlerutils.CodeNotSet, msg, err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, mem)
}

func (h *Handler) CreateMemoriesBatch(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req createMemoriesBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	input := models.BatchCreateMemoryInput{
		Items:     make([]models.CreateMemoryInput, 0, len(req.Items)),
		AllOrNone: req.AllOrNone,
	}
	for _, item := range req.Items {
		createInput, err := toCreateMemoryInput(item)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid UUID in batch request", err)
			return
		}
		input.Items = append(input.Items, createInput)
	}

	memories, err := h.ds.CreateMemoriesBatch(r.Context(), userID, input)
	if err != nil {
		h.logger.Error("failed to create memory batch", zap.String("user_id", userID.String()), zap.Error(err))
		status, msg := classifyMemoryWriteError(err)
		handlerutils.RespondWithError(w, h.logger, status, handlerutils.CodeNotSet, msg, err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, createMemoriesBatchResponse{
		Results:      memories,
		CreatedCount: len(memories),
	})
}
