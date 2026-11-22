package mcpserver

// OAuth connectors set up from their server URL alone (issue #517): discovery of how to sign
// in, client registration, the Client ID Metadata Document this deployment serves, and listing
// a connector's tools once its sign-in completes.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/mcpoauth"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type discoverMCPServerOAuthRequest struct {
	ServerURL string `json:"server_url"`
}

// discoverMCPServerOAuthResponse is what the setup form shows before saving: who the
// connector will sign in with, and how its client will be set up.
type discoverMCPServerOAuthResponse struct {
	*mcpoauth.Discovery
	// Registration is how a client will be got: metadata_document, dynamic, or manual (the
	// person has to register one and type its client ID in).
	Registration string `json:"registration"`
}

// DiscoverMCPServerOAuth reads how to sign in to an MCP server from the server itself. Nothing
// is registered or saved; saving with oauth_auto does that.
func (h *Handler) DiscoverMCPServerOAuth(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.GetUserIDFromContext(r.Context()); !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if h.oauth == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "OAuth service unavailable", nil)
		return
	}
	var req discoverMCPServerOAuthRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || strings.TrimSpace(req.ServerURL) == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "server_url is required", err)
		return
	}
	serverURL := strings.TrimSpace(req.ServerURL)
	if err := validateConnectionTestURL(r.Context(), serverURL, h.config.AllowLocalhostConnections, h.resolveHostIPs); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}
	d, err := h.oauth.Discover(r.Context(), serverURL)
	if err != nil {
		h.respondDiscoveryError(w, err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, discoverMCPServerOAuthResponse{Discovery: d, Registration: h.oauth.RegistrationPlan(d)})
}

// respondDiscoveryError answers a failed discovery or registration with a code the setup form
// branches on: no sign-in needed, a client ID needed, or the settings unreadable (enter by hand).
func (h *Handler) respondDiscoveryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mcpoauth.ErrNoAuthRequired):
		handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, models.ErrCodeMCPOAuthNotRequired,
			"This server didn't ask for sign-in. Use no authentication, or a token.", nil)
	case errors.Is(err, mcpoauth.ErrClientRegistrationUnavailable):
		handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, models.ErrCodeMCPOAuthClientIDRequired,
			"This server's sign-in can't register What Iff by itself. Register it with the provider and enter the client ID.", nil)
	case errors.Is(err, mcpoauth.ErrDiscovery):
		handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, models.ErrCodeMCPOAuthDiscoveryFailed, sentence(err.Error()), nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusBadGateway, models.ErrCodeMCPOAuthDiscoveryFailed, "Couldn't read this server's sign-in settings.", err)
	}
}

// sentence makes an error's text a sentence to show: a capital first letter and a full stop.
func sentence(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return msg
	}
	r, size := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[size:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

// autoConfigure sets server's OAuth settings from its server URL, with the client ID (and
// secret) given if any, else a client registered for it. It answers the request itself on
// failure; the returned secret is the one to store (empty for none).
func (h *Handler) autoConfigure(w http.ResponseWriter, r *http.Request, server *models.MCPServer, clientID, clientSecret string) (string, bool) {
	if h.oauth == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "OAuth service unavailable", nil)
		return "", false
	}
	if err := validateConnectionTestURL(r.Context(), server.ServerURL, h.config.AllowLocalhostConnections, h.resolveHostIPs); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return "", false
	}
	cfg, err := h.oauth.AutoConfigure(r.Context(), server.ServerURL, clientID, clientSecret)
	if err != nil {
		h.respondDiscoveryError(w, err)
		return "", false
	}
	cfg.Apply(server)
	return cfg.Registration.ClientSecret, true
}

// clearDiscovered makes a connector whose OAuth settings were edited by hand a manual one:
// what was discovered no longer describes it.
func clearDiscovered(server *models.MCPServer) {
	server.OAuthResource = ""
	server.OAuthIssuer = ""
	server.OAuthIssParamSupported = false
	server.OAuthClientRegistration = models.MCPOAuthRegistrationManual
}

// ServeClientMetadataDocument serves this deployment's Client ID Metadata Document: the
// client ID authorization servers that take one are given is this document's URL, and they
// fetch it to learn the app's name and redirect URI. Public, like the callback.
func (h *Handler) ServeClientMetadataDocument(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		http.NotFound(w, r)
		return
	}
	doc, ok := h.oauth.ClientMetadataDocument()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, doc)
}

// listToolsAfterSignIn lists a connector's tools right after its sign-in completes, so the
// connector shows what it offers and is marked healthy (or says why not) without a test.
func (h *Handler) listToolsAfterSignIn(ctx context.Context, userID, connectorID uuid.UUID) {
	if h.prober == nil || userID == uuid.Nil || connectorID == uuid.Nil {
		return
	}
	server, err := h.provider.GetMCPServer(ctx, userID, connectorID)
	if err != nil {
		h.logger.Warn("couldn't load a connector after its sign-in", zap.String("mcp_server_id", connectorID.String()), zap.Error(err))
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	toolCount, err := h.prober.ProbeConnection(probeCtx, server)
	now := time.Now().UTC()
	status, reason, healthy := models.MCPServerStatusActive, "", &now
	if err != nil {
		h.logger.Warn("couldn't list a connector's tools after its sign-in", zap.String("mcp_server_id", connectorID.String()), zap.Error(err))
		toolCount, reason, healthy = server.ToolCount, "Signed in, but listing its tools failed: "+err.Error(), server.LastHealthyAt
		if len(reason) > 300 {
			reason = reason[:300]
		}
	}
	if err := h.provider.UpdateMCPServerRuntimeState(ctx, userID, connectorID, status, reason, toolCount, &now, healthy); err != nil {
		h.logger.Warn("couldn't save a connector's tools after its sign-in", zap.String("mcp_server_id", connectorID.String()), zap.Error(err))
	}
}
