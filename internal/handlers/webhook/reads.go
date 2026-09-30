package webhook

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// The read routes. Every one requires the chat:read scope (see RegisterWebhookRoutes) and answers
// for the token owner's own data only. A record that belongs to someone else is reported exactly
// like one that does not exist (404), so a token holder cannot probe for other accounts' ids.

// ListChats returns the owner's threads, newest activity first, as the session API does.
// Filter by persona with personality_id; GET /webhooks/personality lists the personas to choose from.
func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.readerID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, limit, err := pagingFrom(q)
	if err != nil {
		h.badRequest(w, err)
		return
	}
	filters, err := chatFiltersFrom(q)
	if err != nil {
		h.badRequest(w, err)
		return
	}

	result, err := h.provider.ListChats(r.Context(), userID, page, limit, filters)
	if err != nil {
		h.readFailed(w, "failed to list chats", err)
		return
	}
	h.auditRead(r, "list_chats", zap.Int("returned", len(result.Results)))
	h.respondRead(w, result)
}

// ListPersonalities returns the owner's personas as id, name and cover only, enough to pick a
// persona and filter threads by it. A persona's system prompt, scratchpad and memory settings are
// its private working state and are never served here.
func (h *Handler) ListPersonalities(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.readerID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, limit, err := pagingFrom(q)
	if err != nil {
		h.badRequest(w, err)
		return
	}

	result, err := h.provider.ListPersonalities(r.Context(), userID, page, limit, personalityFiltersFrom(q))
	if err != nil {
		h.readFailed(w, "failed to list personalities", err)
		return
	}
	for i, item := range result.Results {
		if p, ok := item.(*models.Personality); ok {
			result.Results[i] = models.NewWebhookPersonality(p)
		}
	}
	h.auditRead(r, "list_personalities", zap.Int("returned", len(result.Results)))
	h.respondRead(w, result)
}

// ListChatMessages returns one thread's messages, newest first. Page with page and limit, or walk
// back through history with cursor: pass the previous response's next_cursor to get the messages
// strictly older than that page (this stays correct while new messages arrive, which page numbers
// do not).
func (h *Handler) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.readerID(w, r)
	if !ok {
		return
	}
	chatID, err := uuid.Parse(mux.Vars(r)["chatId"])
	if err != nil {
		h.badRequest(w, errors.New("chat ID must be a UUID"))
		return
	}
	q := r.URL.Query()
	page, limit, err := pagingFrom(q)
	if err != nil {
		h.badRequest(w, err)
		return
	}
	filters, err := messageFiltersFrom(q)
	if err != nil {
		h.badRequest(w, err)
		return
	}

	var result *models.PaginatedResponse
	if cursor := q.Get("cursor"); cursor != "" {
		sentAt, id, decodeErr := models.DecodeMessageCursor(cursor)
		if decodeErr != nil {
			h.badRequest(w, errors.New("cursor is not valid; use the next_cursor from a previous response"))
			return
		}
		result, err = h.provider.ListChatMessagesBefore(r.Context(), userID, chatID, sentAt, id, limit, filters)
	} else {
		result, err = h.provider.ListChatMessages(r.Context(), userID, chatID, page, limit, filters)
	}
	if err != nil {
		h.readFailed(w, "failed to list messages", err)
		return
	}
	for i, item := range result.Results {
		if m, ok := item.(*models.ChatMessage); ok {
			result.Results[i] = models.NewWebhookMessage(m)
		}
	}
	h.auditRead(r, "list_messages", zap.String("chat_id", chatID.String()), zap.Int("returned", len(result.Results)))
	h.respondRead(w, result)
}

// GetChatMessage returns one message by id. It is how a client reads the sent_at of a message it
// just posted, to match the reply that follows it.
func (h *Handler) GetChatMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.readerID(w, r)
	if !ok {
		return
	}
	messageID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		h.badRequest(w, errors.New("message ID must be a UUID"))
		return
	}

	message, err := h.provider.GetChatMessage(r.Context(), userID, messageID)
	if err != nil {
		h.readFailed(w, "failed to get message", err)
		return
	}
	h.auditRead(r, "get_message", zap.String("message_id", messageID.String()))
	h.respondRead(w, models.NewWebhookMessage(message))
}

// GetJob returns a background job's state. A message posted with mode user or background is
// answered by a job, so a client polls this until status is terminal and then reads result_id.
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.readerID(w, r)
	if !ok {
		return
	}
	jobID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		h.badRequest(w, errors.New("job ID must be a UUID"))
		return
	}

	job, err := h.provider.GetJob(r.Context(), userID, jobID)
	if err != nil {
		h.readFailed(w, "failed to get job", err)
		return
	}
	h.auditRead(r, "get_job", zap.String("job_id", jobID.String()))
	h.respondRead(w, job)
}

// readerID returns the authenticated token owner, or answers 401.
func (h *Handler) readerID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return userID, ok
}

func (h *Handler) badRequest(w http.ResponseWriter, err error) {
	handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
}

// readFailed maps a datastore error to a response. Everything that means "not found or not yours"
// is one 404, with no hint which.
func (h *Handler) readFailed(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, datastore.ErrChatNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
	case errors.Is(err, datastore.ErrChatMessageNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Message not found", nil)
	case errors.Is(err, datastore.ErrJobNotFound), errors.Is(err, datastore.ErrUnauthorized):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Job not found", nil)
	case ent.IsNotFound(err):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, what, err)
	}
}

// respondRead writes a 200 that must not be cached: every read here is live state (new messages,
// job progress), and a stale copy would make a polling client miss them.
func (h *Handler) respondRead(w http.ResponseWriter, body any) {
	w.Header().Set("Cache-Control", "no-store")
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, body)
}

// auditRead records which token read what, never the content. It is the trail for "what has this
// integration been reading", since the token itself is otherwise anonymous in the logs.
func (h *Handler) auditRead(r *http.Request, operation string, fields ...zap.Field) {
	fields = append(fields, zap.String("operation", operation))
	if tokenID, ok := middleware.GetWebhookTokenIDFromContext(r.Context()); ok {
		fields = append(fields, zap.String("webhook_token_id", tokenID.String()))
	}
	h.logger.Info("webhook read", fields...)
}
