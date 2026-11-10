package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

// MCPAppAgent serves MCP Apps' views and their tool calls (ADR 0x042).
type MCPAppAgent interface {
	MCPAppView(ctx context.Context, userID, chatID, toolCallID uuid.UUID) (*agent.MCPAppView, error)
	CallMCPAppTool(ctx context.Context, userID, chatID, toolCallID uuid.UUID, name string, args json.RawMessage) (json.RawMessage, error)
}

const maxAppToolCallBytes = 256 << 10

// mcpAppIDs reads the authenticated user and the chat and tool call ids, or writes the error.
func (h *Handler) mcpAppIDs(w http.ResponseWriter, r *http.Request) (userID, chatID, toolCallID uuid.UUID, ok bool) {
	userID, ok = middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	vars := mux.Vars(r)
	var err error
	if chatID, err = uuid.Parse(vars["chatId"]); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return userID, chatID, toolCallID, false
	}
	if toolCallID, err = uuid.Parse(vars["toolCallId"]); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid tool call ID", err)
		return userID, chatID, toolCallID, false
	}
	if h.appAgent == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "MCP Apps are not available", nil)
		return userID, chatID, toolCallID, false
	}
	return userID, chatID, toolCallID, true
}

func (h *Handler) respondMCPAppError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, datastore.ErrToolCallNotFound), errors.Is(err, agent.ErrNoMCPApp):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "App not found", err)
	case errors.Is(err, mcpclient.ErrToolNotForApps):
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, err.Error(), err)
	case errors.Is(err, agent.ErrMCPAppUnavailable):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadGateway, handlerutils.CodeNotSet, "The app's connector could not be reached", err)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusBadGateway, handlerutils.CodeNotSet, "The app's connector returned an error", err)
	}
}

// GetMCPAppView GET /chat/{chatId}/tool-calls/{toolCallId}/app — a tool call's MCP App view: its
// HTML (with its Content-Security-Policy at the top), the tool's input and result.
func (h *Handler) GetMCPAppView(w http.ResponseWriter, r *http.Request) {
	userID, chatID, toolCallID, ok := h.mcpAppIDs(w, r)
	if !ok {
		return
	}
	view, err := h.appAgent.MCPAppView(r.Context(), userID, chatID, toolCallID)
	if err != nil {
		h.respondMCPAppError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, view)
}

type mcpAppToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallMCPAppTool POST /chat/{chatId}/tool-calls/{toolCallId}/app/tools/call — a tool call the view
// makes on its own connector (MCP Apps tools/call). Returns the CallToolResult as the server sent
// it. 403 when the connector does not let views call that tool.
func (h *Handler) CallMCPAppTool(w http.ResponseWriter, r *http.Request) {
	userID, chatID, toolCallID, ok := h.mcpAppIDs(w, r)
	if !ok {
		return
	}
	var body mcpAppToolCallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAppToolCallBytes)).Decode(&body); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Tool name is required", nil)
		return
	}
	result, err := h.appAgent.CallMCPAppTool(r.Context(), userID, chatID, toolCallID, strings.TrimSpace(body.Name), body.Arguments)
	if err != nil {
		h.respondMCPAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result)
}
