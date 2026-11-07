package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// PromptAgent answers the questions a running turn asks (ADR 0x042).
type PromptAgent interface {
	AnswerPrompt(ctx context.Context, userID, chatID, promptID uuid.UUID, resp models.AgentPromptResponse) (*models.AgentPrompt, error)
}

// maxPromptAnswerBytes bounds an answer's request body.
const maxPromptAnswerBytes = 64 << 10

// AnswerPrompt POST /chat/{chatId}/prompts/{promptId}/answer — the person's answer to a question
// the turn running in this chat asked them (ask_user or an MCP server's elicitation). 409 when the
// prompt no longer takes an answer (answered elsewhere, expired, or its turn ended).
func (h *Handler) AnswerPrompt(w http.ResponseWriter, r *http.Request) {
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
	promptID, err := uuid.Parse(vars["promptId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid prompt ID", err)
		return
	}
	var body models.AgentPromptResponse
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPromptAnswerBytes)).Decode(&body); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if h.promptAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "Prompts are not available", nil)
		return
	}
	prompt, err := h.promptAgent.AnswerPrompt(r.Context(), userID, chatID, promptID, body)
	switch {
	case errors.Is(err, datastore.ErrAgentPromptNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Prompt not found", err)
		return
	case errors.Is(err, datastore.ErrAgentPromptClosed):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "This question is no longer waiting for an answer", err)
		return
	case errors.Is(err, agent.ErrInvalidPromptResponse):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), err)
		return
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to record the answer", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, prompt)
}
