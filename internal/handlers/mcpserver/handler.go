package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
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

type Handler struct {
	provider Provider
	prober   ConnectionProber
	logger   *zap.Logger
}

type ConnectionProber interface {
	ProbeConnection(ctx context.Context, server *models.MCPServer) (int, error)
}

func NewHandler(provider Provider, prober ConnectionProber, logger *zap.Logger) *Handler {
	return &Handler{
		provider: provider,
		prober:   prober,
		logger:   logger,
	}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	for _, prefix := range []string{"/mcp-servers", "/mcp-server"} {
		mcpRouter := router.PathPrefix(prefix).Subrouter()
		mcpRouter.HandleFunc("", h.CreateMCPServer).Methods("POST")
		mcpRouter.HandleFunc("", h.ListMCPServers).Methods("GET")
		mcpRouter.HandleFunc("/test-connection", h.TestMCPServerConnection).Methods("POST")
		mcpRouter.HandleFunc("/{id}", h.GetMCPServer).Methods("GET")
		mcpRouter.HandleFunc("/{id}", h.UpdateMCPServer).Methods("PUT")
		mcpRouter.HandleFunc("/{id}", h.UpdateMCPServer).Methods("PATCH")
		mcpRouter.HandleFunc("/{id}", h.DeleteMCPServer).Methods("DELETE")
	}
}

type createMCPServerRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	ServerURL      string `json:"server_url"`
	Authentication string `json:"authentication"`
	DefaultEnabled bool   `json:"default_enabled"`
}

type testMCPServerConnectionRequest struct {
	ServerURL      string              `json:"server_url"`
	Authentication nullableStringField `json:"authentication,omitempty"`
	ConnectorID    *uuid.UUID          `json:"connector_id,omitempty"`
}

type testMCPServerConnectionResponse struct {
	Pass      bool   `json:"pass"`
	ToolCount int    `json:"tool_count"`
	Message   string `json:"message,omitempty"`
}

func (h *Handler) CreateMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req createMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	name := strings.TrimSpace(req.Name)
	description := strings.TrimSpace(req.Description)
	serverURL := strings.TrimSpace(req.ServerURL)
	if name == "" || description == "" || serverURL == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "name, description, and server_url are required", nil)
		return
	}

	server, err := h.provider.CreateMCPServer(r.Context(), userID, models.MCPServer{
		Name:           name,
		Description:    description,
		ServerURL:      serverURL,
		AuthToken:      strings.TrimSpace(req.Authentication),
		DefaultEnabled: req.DefaultEnabled,
	})
	if err != nil {
		h.logger.Error("failed to create mcp server", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create MCP server", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, server)
}

func (h *Handler) TestMCPServerConnection(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	if h.prober == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "MCP connection testing is unavailable", nil)
		return
	}

	var req testMCPServerConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	serverURL := strings.TrimSpace(req.ServerURL)
	if serverURL == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "server_url is required", nil)
		return
	}

	testServer := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "test-connection",
		ServerURL: serverURL,
		Status:    models.MCPServerStatusActive,
	}
	if req.ConnectorID != nil {
		testServer.ID = *req.ConnectorID
	}

	if req.Authentication.IsSet {
		if req.Authentication.Value != nil {
			testServer.AuthToken = strings.TrimSpace(*req.Authentication.Value)
		}
	} else if req.ConnectorID != nil {
		current, err := h.provider.GetMCPServer(r.Context(), userID, *req.ConnectorID)
		if ent.IsNotFound(err) || err == datastore.ErrMCPServerNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "MCP server not found", err)
			return
		}
		if err != nil {
			h.logger.Error("failed to load mcp server for connection test", zap.String("user_id", userID.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to test MCP server connection", err)
			return
		}
		testServer.AuthToken = strings.TrimSpace(current.AuthToken)
	}

	toolCount, err := h.prober.ProbeConnection(r.Context(), testServer)
	if err != nil {
		h.logger.Warn("mcp test connection failed",
			zap.String("user_id", userID.String()),
			zap.String("server_url", testServer.ServerURL),
			zap.Error(err),
		)
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, testMCPServerConnectionResponse{
			Pass:      false,
			ToolCount: 0,
			Message:   err.Error(),
		})
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, testMCPServerConnectionResponse{
		Pass:      true,
		ToolCount: toolCount,
		Message:   "Connected successfully",
	})
}

func (h *Handler) ListMCPServers(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	filters := models.MCPServerFilters{}
	if q := strings.TrimSpace(queryParams.Get("search")); q != "" {
		filters.Query = &q
	}

	resp, err := h.provider.ListMCPServers(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list mcp servers", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list MCP servers", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, resp)
}

func (h *Handler) GetMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id, ok := parseMCPServerID(w, h.logger, r)
	if !ok {
		return
	}

	server, err := h.provider.GetMCPServer(r.Context(), userID, id)
	if ent.IsNotFound(err) || err == datastore.ErrMCPServerNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "MCP server not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to get mcp server", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get MCP server", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, server)
}

type updateMCPServerRequest struct {
	Name           *string             `json:"name,omitempty"`
	Description    *string             `json:"description,omitempty"`
	ServerURL      *string             `json:"server_url,omitempty"`
	Authentication nullableStringField `json:"authentication,omitempty"`
	DefaultEnabled *bool               `json:"default_enabled,omitempty"`
	RitualIDs      *[]uuid.UUID        `json:"ritual_ids,omitempty"`
}

type nullableStringField struct {
	IsSet bool
	Value *string
}

func (f *nullableStringField) UnmarshalJSON(data []byte) error {
	f.IsSet = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	f.Value = &v
	return nil
}

func (h *Handler) UpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id, ok := parseMCPServerID(w, h.logger, r)
	if !ok {
		return
	}

	var req updateMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	current, err := h.provider.GetMCPServer(r.Context(), userID, id)
	if ent.IsNotFound(err) || err == datastore.ErrMCPServerNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "MCP server not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to load mcp server for update", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update MCP server", err)
		return
	}

	updated := *current
	if req.Name != nil {
		updated.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		updated.Description = strings.TrimSpace(*req.Description)
	}
	if req.ServerURL != nil {
		updated.ServerURL = strings.TrimSpace(*req.ServerURL)
	}
	if req.DefaultEnabled != nil {
		updated.DefaultEnabled = *req.DefaultEnabled
	}

	if updated.Name == "" || updated.Description == "" || updated.ServerURL == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "name, description, and server_url are required", nil)
		return
	}

	authTokenUpdate := models.MCPServerAuthTokenUpdate{}
	if req.Authentication.IsSet {
		authTokenUpdate.Provided = true
		if req.Authentication.Value == nil {
			authTokenUpdate.Clear = true
		} else {
			authTokenUpdate.Value = *req.Authentication.Value
		}
	}

	server, err := h.provider.UpdateMCPServer(r.Context(), userID, updated, authTokenUpdate, req.RitualIDs)
	if ent.IsNotFound(err) || err == datastore.ErrMCPServerNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "MCP server not found", err)
		return
	}
	if errors.Is(err, datastore.ErrInvalidRequestBody) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual_ids", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to update mcp server", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update MCP server", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, server)
}

func (h *Handler) DeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id, ok := parseMCPServerID(w, h.logger, r)
	if !ok {
		return
	}

	err := h.provider.DeleteMCPServer(r.Context(), userID, id)
	if ent.IsNotFound(err) || err == datastore.ErrMCPServerNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "MCP server not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to delete mcp server", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete MCP server", err)
		return
	}

	handlerutils.RespondWithNoContent(w)
}

func parseMCPServerID(w http.ResponseWriter, logger *zap.Logger, r *http.Request) (uuid.UUID, bool) {
	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid MCP server ID", err)
		return uuid.Nil, false
	}
	return id, true
}
