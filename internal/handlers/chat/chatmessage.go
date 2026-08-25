package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type createChatMessageRequest struct {
	Message        string                   `json:"message"`
	Origin         models.MessageOrigin     `json:"origin"`
	ResponseID     *string                  `json:"response_id,omitempty"`
	Attachments    []*models.FileAttachment `json:"attachments,omitempty"`
	Rituals        []*models.Ritual         `json:"rituals,omitempty"`
	ClientTimezone string                   `json:"client_timezone,omitempty"`
}

// CreateChatMessage creates a new chat message from the user and initiates the agent processing
func (h *Handler) CreateChatMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.GetUserIDFromContext(r.Context()); !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	chatIDStr := vars["chatId"]

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	// Parse request body
	var req createChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if handlerutils.UTF16CodeUnitCount(req.Message) > handlerutils.TextLimitHardMax {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, models.ErrCodeMessageTooLong,
			fmt.Sprintf("Message exceeds maximum length of %d characters", handlerutils.TextLimitHardMax), nil)
		return
	}

	// Usage limits are enforced end-to-end by the agent's metered path (the meter's
	// gate → usage accounting), which returns agent.ErrQuotaExceeded below when a
	// limit is reached. Imported conversations write message rows but are never
	// metered, so they do not affect usage limits.

	msg := models.ChatMessage{
		ChatID:      chatID,
		Message:     req.Message,
		Origin:      req.Origin,
		ResponseID:  req.ResponseID,
		Attachments: req.Attachments,
		Rituals:     req.Rituals,
	}

	// Call the agent to handle the message. Prefer client timezone when provided.
	// The authenticated user's saved timezone (if any) is injected into context by AuthMiddleware.
	ctx := r.Context()
	if req.ClientTimezone != "" {
		ctx = context.WithValue(ctx, middleware.ClientTimezoneKey, req.ClientTimezone)
	}
	if h.messageAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Message agent not configured", nil)
		return
	}

	response, err := h.messageAgent.HandleUserMessage(ctx, msg)
	if err != nil {
		if errors.Is(err, agent.ErrQuotaExceeded) {
			handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, models.ErrCodeQuotaExceeded,
				"You've used all of your free trial credits. Please subscribe to continue chatting.", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to process message", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, response)
}

// RetryChatMessage POST /chat/{chatId}/chat-message/{messageId}/retry — re-run generation for an existing user turn.
func (h *Handler) RetryChatMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.GetUserIDFromContext(r.Context()); !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	vars := mux.Vars(r)
	chatID, err := uuid.Parse(vars["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}
	messageID, err := uuid.Parse(vars["messageId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid message ID", err)
		return
	}
	if h.messageAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Message agent not configured", nil)
		return
	}
	resp, err := h.messageAgent.RetryUserChatMessage(r.Context(), chatID, messageID)
	if err != nil {
		if errors.Is(err, datastore.ErrChatMessageNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat message not found", err)
			return
		}
		if errors.Is(err, agent.ErrRetryChatMismatch) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat message not found in this chat", err)
			return
		}
		if errors.Is(err, agent.ErrRetryNotUserOrigin) {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Only user messages can be retried", err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to retry message", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, resp)
}

// GetActiveChatMessageJob GET /chat/{chatId}/chat-message/{messageId}/active-job — non-terminal job for resume after refresh.
func (h *Handler) GetActiveChatMessageJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	vars := mux.Vars(r)
	chatID, err := uuid.Parse(vars["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}
	messageID, err := uuid.Parse(vars["messageId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid message ID", err)
		return
	}
	msg, err := h.ds.GetChatMessage(r.Context(), userID, messageID)
	if err != nil {
		if errors.Is(err, datastore.ErrChatMessageNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat message not found", err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat message", err)
		return
	}
	if msg.ChatID != chatID {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat message not found in this chat", nil)
		return
	}
	j, err := h.ds.FindLatestActiveChatMessageJob(r.Context(), userID, messageID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to look up job", err)
		return
	}
	if j == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, models.ActiveChatMessageJobResponse{
		JobID:  j.ID,
		Status: j.Status,
	})
}

// GetChatMessage handler function for GET /chat-message/{id}
func (h *Handler) GetChatMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]

	messageID, err := uuid.Parse(id)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat message ID", err)
		return
	}

	respMessage, err := h.ds.GetChatMessage(r.Context(), userID, messageID)
	if ent.IsNotFound(err) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat message not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get recipe", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "failed to get chat message", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, respMessage)
}

// GetChatMessages handler function for GET /chat-message
func (h *Handler) GetChatMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	chatIDStr := vars["chatId"]

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	queryParams := r.URL.Query()

	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	origin := queryParams.Get("origin")
	searchQuery := queryParams.Get("search")
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")

	filters := models.ChatMessageFilters{}

	if origin != "" {
		msgOrigin := models.MessageOrigin(origin)
		filters.Origin = &msgOrigin
	}

	if searchQuery != "" {
		filters.Query = &searchQuery
	}

	if minDateStr != "" {
		minDate, err := time.Parse(time.RFC3339, minDateStr)
		if err == nil {
			filters.MinDate = &minDate
		}
	}

	if maxDateStr != "" {
		maxDate, err := time.Parse(time.RFC3339, maxDateStr)
		if err == nil {
			filters.MaxDate = &maxDate
		}
	}

	messagePage, err := h.ds.ListChatMessages(r.Context(), userID, chatID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list chat messages", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "failed to list chat messages", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, messagePage)
}
