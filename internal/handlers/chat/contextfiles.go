package chat

import (
	"errors"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

// ContextFilesResponse is the body of GET /chat/{chatId}/context-files.
type ContextFilesResponse struct {
	Files []agent.ContextFile `json:"files"`
	// Container is what the private build reports about the conversation's container; absent in
	// the public build.
	Container *agent.ContainerStatus `json:"container,omitempty"`
}

// ListChatContextFiles handles GET /chat/{chatId}/context-files (ADR 0x043 §4): the files an agent
// working in the thread is expected to see, by the same rule the private build hydrates its
// container with (agent.ContextFilesForChat).
func (h *Handler) ListChatContextFiles(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}
	chat, err := h.ds.GetChat(r.Context(), userID, chatID)
	if err != nil {
		if ent.IsNotFound(err) || errors.Is(err, datastore.ErrChatNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat", err)
		return
	}
	if h.agent == nil {
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, ContextFilesResponse{Files: []agent.ContextFile{}})
		return
	}
	files, err := h.agent.ContextFilesForChat(r.Context(), chat)
	if err != nil {
		h.logger.Error("failed to list the chat's context files", zap.String("chat_id", chatID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list the thread's files", err)
		return
	}
	if files == nil {
		files = []agent.ContextFile{}
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, ContextFilesResponse{Files: files, Container: h.agent.ContainerStatusForChat(r.Context(), chat, files)})
}
