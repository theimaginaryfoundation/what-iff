package chat

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func (h *Handler) ListChatMCPServers(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}

	servers, err := h.ds.ListChatMCPServers(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to list chat mcp servers",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list chat MCP servers", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, servers)
}

func (h *Handler) ListAvailableChatMCPServers(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}

	queryParams := r.URL.Query()
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	filters := models.MCPServerFilters{}
	if q := strings.TrimSpace(queryParams.Get("search")); q != "" {
		filters.Query = &q
	}

	resp, err := h.ds.ListAvailableChatMCPServers(r.Context(), userID, chatID, page, pageSize, filters)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to list available chat mcp servers",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list available chat MCP servers", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, resp)
}

func (h *Handler) AddMCPServerToChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}
	mcpServerID, ok := parseChatIDVar(w, h.logger, r, "mcpServerId")
	if !ok {
		return
	}

	err := h.ds.AddMCPServerToChat(r.Context(), userID, chatID, mcpServerID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound || err == datastore.ErrMCPServerNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat or MCP server not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to add mcp server to chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.String("mcp_server_id", mcpServerID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to add MCP server to chat", err)
		return
	}

	handlerutils.RespondWithNoContent(w)
}

func (h *Handler) RemoveMCPServerFromChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}
	mcpServerID, ok := parseChatIDVar(w, h.logger, r, "mcpServerId")
	if !ok {
		return
	}

	err := h.ds.RemoveMCPServerFromChat(r.Context(), userID, chatID, mcpServerID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound || err == datastore.ErrMCPServerNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat or MCP server not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to remove mcp server from chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.String("mcp_server_id", mcpServerID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to remove MCP server from chat", err)
		return
	}

	handlerutils.RespondWithNoContent(w)
}

func parseChatIDVar(w http.ResponseWriter, logger *zap.Logger, r *http.Request, key string) (uuid.UUID, bool) {
	idStr := mux.Vars(r)[key]
	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid "+key, err)
		return uuid.Nil, false
	}
	return id, true
}
