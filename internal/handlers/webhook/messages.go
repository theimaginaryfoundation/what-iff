package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func (h *Handler) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	chatID, err := uuid.Parse(mux.Vars(r)["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	var req models.WebhookChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	req.Mode = models.WebhookMessageMode(strings.ToLower(strings.TrimSpace(string(req.Mode))))
	if req.Message == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Message is required", nil)
		return
	}

	ctx := r.Context()
	if req.ClientTimezone != "" {
		ctx = context.WithValue(ctx, middleware.ClientTimezoneKey, req.ClientTimezone)
	}

	switch req.Mode {
	case models.WebhookMessageModeUser:
		h.handleWebhookUserMode(w, ctx, req, chatID)
	case models.WebhookMessageModeAssistant:
		h.handleWebhookAssistantMode(w, ctx, userID, req, chatID)
	case models.WebhookMessageModeBackground:
		h.handleWebhookBackgroundMode(w, ctx, req, chatID)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "mode must be one of: user, assistant, background", nil)
	}
}

func (h *Handler) handleWebhookUserMode(w http.ResponseWriter, ctx context.Context, req models.WebhookChatMessageRequest, chatID uuid.UUID) {
	response, err := h.agent.HandleUserMessage(ctx, models.ChatMessage{
		ChatID:      chatID,
		Message:     req.Message,
		Origin:      models.MessageOriginUser,
		ResponseID:  req.ResponseID,
		Attachments: req.Attachments,
		Rituals:     req.Rituals,
	})
	if err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to process webhook user message", err)
		return
	}

	jobID := response.JobID
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.WebhookChatMessageResponse{
		ID:    response.ID,
		JobID: &jobID,
		Type:  response.Type,
		Mode:  models.WebhookMessageModeUser,
	})
}

func (h *Handler) handleWebhookAssistantMode(w http.ResponseWriter, ctx context.Context, userID uuid.UUID, req models.WebhookChatMessageRequest, chatID uuid.UUID) {
	chatMessage, err := h.provider.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:                chatID,
		Message:               req.Message,
		Origin:                models.MessageOriginAssistant,
		ResponseID:            req.ResponseID,
		Attachments:           req.Attachments,
		GenerationModel:       "none",
		GenerationPersonality: "webhook",
	})
	if err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to write assistant webhook message", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, models.WebhookChatMessageResponse{
		ID:   chatMessage.ID,
		Type: "webhook_assistant",
		Mode: models.WebhookMessageModeAssistant,
	})
}

func (h *Handler) handleWebhookBackgroundMode(w http.ResponseWriter, ctx context.Context, req models.WebhookChatMessageRequest, chatID uuid.UUID) {
	response, err := h.agent.HandleAgentJobPromptAsync(ctx, chatID, req.Message, nil, nil)
	if err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to run background webhook message", err)
		return
	}

	jobID := response.JobID
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, models.WebhookChatMessageResponse{
		ID:    response.ID,
		JobID: &jobID,
		Type:  response.Type,
		Mode:  models.WebhookMessageModeBackground,
	})
}
