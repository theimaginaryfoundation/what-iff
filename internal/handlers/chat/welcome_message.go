package chat

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

// CreateWelcomeMessage triggers a first-chat welcome message generation as a background job.
// It is a no-op (204) when the user already has chat messages or when the target is not the first chat.
func (h *Handler) CreateWelcomeMessage(w http.ResponseWriter, r *http.Request) {
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

	if h.welcomeAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Welcome message agent not configured", nil)
		return
	}

	if _, err := h.ds.GetChat(r.Context(), userID, chatID); err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat", err)
		return
	}

	isFirstChat, err := h.ds.IsFirstChat(r.Context(), userID, chatID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to evaluate chat eligibility", err)
		return
	}
	if !isFirstChat {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	msgCount, err := h.ds.CountAllChatMessages(r.Context(), userID, 1)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to evaluate message eligibility", err)
		return
	}
	if msgCount > 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	modelOverrideID := h.resolveFirstChatGreetingModelID(r.Context(), userID)
	resp, err := h.welcomeAgent.HandleWelcomeMessagePromptAsync(
		r.Context(),
		chatID,
		agent.BuildFirstChatGreetingPrompt(),
		modelOverrideID,
		nil,
	)
	if err != nil {
		h.logger.Warn("failed to enqueue welcome message job",
			zap.String("context", firstChatGreetingPromptLogContext),
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create welcome message", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, resp)
}
