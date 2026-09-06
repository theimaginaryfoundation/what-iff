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
	"github.com/theimaginaryfoundation/what-iff/internal/mcpoauth"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type Handler struct {
	provider Provider
	prober   ConnectionProber
	oauth    OAuthService
	logger   *zap.Logger
}

type ConnectionProber interface {
	ProbeConnection(ctx context.Context, server *models.MCPServer) (int, error)
}

func NewHandler(provider Provider, prober ConnectionProber, oauth OAuthService, logger *zap.Logger) *Handler {
	return &Handler{
		provider: provider,
		prober:   prober,
		oauth:    oauth,
		logger:   logger,
	}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	for _, prefix := range []string{"/mcp-servers", "/mcp-server"} {
		mcpRouter := router.PathPrefix(prefix).Subrouter()
		mcpRouter.HandleFunc("", h.CreateMCPServer).Methods("POST")
		mcpRouter.HandleFunc("", h.ListMCPServers).Methods("GET")
		mcpRouter.HandleFunc("/test-connection", h.TestMCPServerConnection).Methods("POST")
		mcpRouter.HandleFunc("/{id}/oauth/start", h.StartMCPServerOAuth).Methods("POST")
		mcpRouter.HandleFunc("/{id}", h.GetMCPServer).Methods("GET")
		mcpRouter.HandleFunc("/{id}", h.UpdateMCPServer).Methods("PUT")
		mcpRouter.HandleFunc("/{id}", h.UpdateMCPServer).Methods("PATCH")
		mcpRouter.HandleFunc("/{id}", h.DeleteMCPServer).Methods("DELETE")
	}
}

func (h *Handler) RegisterPublicRoutes(router *mux.Router) {
	for _, prefix := range []string{"/mcp-servers", "/mcp-server"} {
		mcpRouter := router.PathPrefix(prefix).Subrouter()
		mcpRouter.HandleFunc("/oauth/callback", h.HandleMCPServerOAuthCallback).Methods("GET")
	}
}

type createMCPServerRequest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	ServerURL         string   `json:"server_url"`
	AuthMode          string   `json:"auth_mode"`
	Authentication    string   `json:"authentication"`
	DefaultEnabled    bool     `json:"default_enabled"`
	OAuthAuthURL      string   `json:"oauth_auth_url"`
	OAuthTokenURL     string   `json:"oauth_token_url"`
	OAuthClientID     string   `json:"oauth_client_id"`
	OAuthClientSecret string   `json:"oauth_client_secret"`
	OAuthScopes       []string `json:"oauth_scopes"`
	OAuthPKCEPolicy   string   `json:"oauth_pkce_policy"`
}

type testMCPServerConnectionRequest struct {
	ServerURL      string              `json:"server_url"`
	AuthMode       string              `json:"auth_mode,omitempty"`
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
	authMode := strings.TrimSpace(req.AuthMode)
	if authMode == "" {
		authMode = models.MCPServerAuthModeHeader
	}
	pkcePolicy := strings.TrimSpace(req.OAuthPKCEPolicy)
	if pkcePolicy == "" {
		pkcePolicy = models.MCPServerPKCESupported
	}
	if name == "" || description == "" || serverURL == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "name, description, and server_url are required", nil)
		return
	}
	if authMode != models.MCPServerAuthModeHeader && authMode != models.MCPServerAuthModeOAuth {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "auth_mode must be 'header' or 'oauth'", nil)
		return
	}
	if authMode == models.MCPServerAuthModeOAuth {
		if strings.TrimSpace(req.OAuthAuthURL) == "" || strings.TrimSpace(req.OAuthTokenURL) == "" || strings.TrimSpace(req.OAuthClientID) == "" {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "oauth_auth_url, oauth_token_url, and oauth_client_id are required for oauth mode", nil)
			return
		}
		if !validPKCEPolicy(pkcePolicy) {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "oauth_pkce_policy must be required, supported, or not_supported", nil)
			return
		}
	}

	server, err := h.provider.CreateMCPServer(r.Context(), userID, models.MCPServer{
		Name:              name,
		Description:       description,
		ServerURL:         serverURL,
		AuthMode:          authMode,
		AuthToken:         strings.TrimSpace(req.Authentication),
		DefaultEnabled:    req.DefaultEnabled,
		OAuthAuthURL:      strings.TrimSpace(req.OAuthAuthURL),
		OAuthTokenURL:     strings.TrimSpace(req.OAuthTokenURL),
		OAuthClientID:     strings.TrimSpace(req.OAuthClientID),
		OAuthClientSecret: strings.TrimSpace(req.OAuthClientSecret),
		OAuthScopes:       normalizeScopes(req.OAuthScopes),
		OAuthPKCEPolicy:   pkcePolicy,
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
		AuthMode:  models.MCPServerAuthModeHeader,
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
		testServer.AuthMode = strings.TrimSpace(current.AuthMode)
		testServer.OAuthAccessToken = strings.TrimSpace(current.OAuthAccessToken)
	}
	if mode := strings.TrimSpace(req.AuthMode); mode != "" {
		testServer.AuthMode = mode
	}
	if strings.TrimSpace(testServer.AuthMode) == models.MCPServerAuthModeOAuth {
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, testMCPServerConnectionResponse{
			Pass:      false,
			ToolCount: 0,
			Message:   "OAuth connectors require Authenticate/Reauthenticate before testing tool discovery.",
		})
		return
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
	Name              *string             `json:"name,omitempty"`
	Description       *string             `json:"description,omitempty"`
	ServerURL         *string             `json:"server_url,omitempty"`
	AuthMode          *string             `json:"auth_mode,omitempty"`
	Authentication    nullableStringField `json:"authentication,omitempty"`
	DefaultEnabled    *bool               `json:"default_enabled,omitempty"`
	RitualIDs         *[]uuid.UUID        `json:"ritual_ids,omitempty"`
	OAuthAuthURL      *string             `json:"oauth_auth_url,omitempty"`
	OAuthTokenURL     *string             `json:"oauth_token_url,omitempty"`
	OAuthClientID     *string             `json:"oauth_client_id,omitempty"`
	OAuthClientSecret nullableStringField `json:"oauth_client_secret,omitempty"`
	OAuthScopes       *[]string           `json:"oauth_scopes,omitempty"`
	OAuthPKCEPolicy   *string             `json:"oauth_pkce_policy,omitempty"`
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
	if req.AuthMode != nil {
		updated.AuthMode = strings.TrimSpace(*req.AuthMode)
	}
	if req.OAuthAuthURL != nil {
		updated.OAuthAuthURL = strings.TrimSpace(*req.OAuthAuthURL)
	}
	if req.OAuthTokenURL != nil {
		updated.OAuthTokenURL = strings.TrimSpace(*req.OAuthTokenURL)
	}
	if req.OAuthClientID != nil {
		updated.OAuthClientID = strings.TrimSpace(*req.OAuthClientID)
	}
	if req.OAuthScopes != nil {
		updated.OAuthScopes = normalizeScopes(*req.OAuthScopes)
	}
	if req.OAuthPKCEPolicy != nil {
		updated.OAuthPKCEPolicy = strings.TrimSpace(*req.OAuthPKCEPolicy)
	}

	if updated.Name == "" || updated.Description == "" || updated.ServerURL == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "name, description, and server_url are required", nil)
		return
	}
	if strings.TrimSpace(updated.AuthMode) == "" {
		updated.AuthMode = models.MCPServerAuthModeHeader
	}
	if updated.AuthMode != models.MCPServerAuthModeHeader && updated.AuthMode != models.MCPServerAuthModeOAuth {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "auth_mode must be 'header' or 'oauth'", nil)
		return
	}
	if updated.AuthMode == models.MCPServerAuthModeOAuth {
		if strings.TrimSpace(updated.OAuthAuthURL) == "" || strings.TrimSpace(updated.OAuthTokenURL) == "" || strings.TrimSpace(updated.OAuthClientID) == "" {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "oauth_auth_url, oauth_token_url, and oauth_client_id are required for oauth mode", nil)
			return
		}
		if strings.TrimSpace(updated.OAuthPKCEPolicy) == "" {
			updated.OAuthPKCEPolicy = models.MCPServerPKCESupported
		}
		if !validPKCEPolicy(updated.OAuthPKCEPolicy) {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "oauth_pkce_policy must be required, supported, or not_supported", nil)
			return
		}
	}

	authTokenUpdate := models.MCPServerAuthTokenUpdate{}
	oauthSecretUpdate := models.MCPOAuthSecretUpdate{}
	if req.Authentication.IsSet {
		authTokenUpdate.Provided = true
		if req.Authentication.Value == nil {
			authTokenUpdate.Clear = true
		} else {
			authTokenUpdate.Value = *req.Authentication.Value
		}
	}
	if req.OAuthClientSecret.IsSet {
		oauthSecretUpdate.Provided = true
		if req.OAuthClientSecret.Value == nil {
			oauthSecretUpdate.Clear = true
		} else {
			oauthSecretUpdate.Value = strings.TrimSpace(*req.OAuthClientSecret.Value)
		}
	}

	server, err := h.provider.UpdateMCPServer(r.Context(), userID, updated, authTokenUpdate, oauthSecretUpdate, req.RitualIDs)
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

type startMCPServerOAuthRequest struct {
	RedirectAfter string `json:"redirect_after,omitempty"`
}

type startMCPServerOAuthResponse struct {
	AuthorizationURL string `json:"authorization_url"`
}

func (h *Handler) StartMCPServerOAuth(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if h.oauth == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "OAuth service unavailable", nil)
		return
	}
	id, ok := parseMCPServerID(w, h.logger, r)
	if !ok {
		return
	}
	var req startMCPServerOAuthRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	authURL, err := h.oauth.StartAuth(r.Context(), userID, id, strings.TrimSpace(req.RedirectAfter))
	if errors.Is(err, datastore.ErrMCPServerNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "MCP server not found", err)
		return
	}
	if errors.Is(err, datastore.ErrMCPOAuthConnectorNotReady) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Connector OAuth configuration is incomplete", err)
		return
	}
	if errors.Is(err, mcpoauth.ErrInvalidRedirectAfter) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "redirect_after must be on an allowed origin", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to start connector oauth", zap.String("user_id", userID.String()), zap.String("mcp_server_id", id.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to start connector authentication", nil)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, startMCPServerOAuthResponse{AuthorizationURL: authURL})
}

func (h *Handler) HandleMCPServerOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "OAuth service unavailable", nil)
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	oauthErr := strings.TrimSpace(r.URL.Query().Get("error"))
	oauthErrDesc := strings.TrimSpace(r.URL.Query().Get("error_description"))
	redirectURL, _, _, err := h.oauth.HandleCallback(r.Context(), state, code, oauthErr, oauthErrDesc)
	if err != nil {
		if strings.TrimSpace(redirectURL) != "" {
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}
		if errors.Is(err, datastore.ErrMCPOAuthSessionNotFound) ||
			errors.Is(err, datastore.ErrMCPOAuthSessionExpired) ||
			errors.Is(err, datastore.ErrMCPOAuthSessionConsumed) {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "OAuth session is invalid or expired. Start authentication again.", nil)
			return
		}
		h.logger.Error("failed to process connector oauth callback", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to process OAuth callback", nil)
		return
	}
	if strings.TrimSpace(redirectURL) == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to process OAuth callback", nil)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
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

func validPKCEPolicy(v string) bool {
	switch strings.TrimSpace(v) {
	case models.MCPServerPKCERequired, models.MCPServerPKCESupported, models.MCPServerPKCENotSupported:
		return true
	default:
		return false
	}
}

func normalizeScopes(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
