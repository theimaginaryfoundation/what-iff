package mcpoauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type Store interface {
	GetMCPServer(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error)
	CreateMCPOAuthSession(ctx context.Context, userID, mcpServerID uuid.UUID, state, codeVerifier, redirectAfter string, expiresAt time.Time) (*models.MCPOAuthSession, error)
	ConsumeMCPOAuthSessionByState(ctx context.Context, state string) (*models.MCPOAuthSession, *models.MCPServer, error)
	SaveMCPServerOAuthTokens(ctx context.Context, userID, mcpServerID uuid.UUID, set models.MCPOAuthTokenSet) error
	MarkMCPServerOAuthRefreshFailure(ctx context.Context, userID, mcpServerID uuid.UUID, reason string, terminal bool) error
	ListOAuthMCPServersDueForRefresh(ctx context.Context, before time.Time, limit int) ([]*models.MCPServer, error)
	MarkMCPServerOAuthExpiring(ctx context.Context, userID, mcpServerID uuid.UUID, reason string) error
}

type Service struct {
	store               Store
	httpClient          *http.Client
	logger              *zap.Logger
	redirectURL         string
	postAuthRedirectURL string
	allowedRedirects    map[string]struct{}
	// discoveryClient makes every discovery and registration request (issue #517): public
	// addresses only, never the credential transport.
	discoveryClient   *http.Client
	allowLocalhost    bool
	clientMetadataURL string
	clientName        string
}

type Config struct {
	RedirectURL         string
	PostAuthRedirectURL string
	AllowedRedirects    []string
	// ClientMetadataURL is the https URL this deployment's Client ID Metadata Document is
	// served at (issue #517). Empty derives it from RedirectURL when that is an https
	// ".../oauth/callback"; a deployment without a public https address has none, and
	// discovered connectors fall back to dynamic registration.
	ClientMetadataURL string
	// ClientName is the app's name in that document and in dynamic registrations.
	ClientName string
	// AllowLocalhost lets discovery reach MCP and sign-in servers on loopback (local development),
	// as MCP_ALLOW_LOCALHOST_CONNECTIONS does for connection tests.
	AllowLocalhost bool
}

// clientMetadataPath is where the Client ID Metadata Document is served, beside the callback.
const clientMetadataPath = "/oauth/client-metadata.json"

// clientMetadataURLFor is the metadata document's URL: configured, else next to an https
// callback; empty when there is no public https address for it.
func clientMetadataURLFor(configured, callback string) string {
	if c := strings.TrimSpace(configured); c != "" {
		u, err := url.Parse(c)
		if err != nil || u.Scheme != "https" || u.Host == "" || strings.Trim(u.Path, "/") == "" {
			return ""
		}
		return u.String()
	}
	u, err := url.Parse(callback)
	if err != nil || u.Scheme != "https" || u.Host == "" || !strings.HasSuffix(u.Path, "/oauth/callback") {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, "/oauth/callback") + clientMetadataPath
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

var (
	oauthMessagePolicy      = bluemonday.StrictPolicy()
	oauthControlCharsRegex  = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
	ErrInvalidRedirectAfter = errors.New("invalid redirect_after")
)

const defaultOAuthCallbackURL = "http://localhost:8080/api/mcp-servers/oauth/callback"

func New(store Store, client *http.Client, logger *zap.Logger, cfg Config) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	redirectURL := sanitizeCallbackRedirectURL(strings.TrimSpace(cfg.RedirectURL), logger)
	safePostAuthURL := sanitizePostAuthRedirectURL(strings.TrimSpace(cfg.PostAuthRedirectURL))
	allowed := normalizeAllowedRedirectOrigins(cfg.AllowedRedirects)
	allowed[redirectOrigin(safePostAuthURL)] = struct{}{}

	clientName := strings.TrimSpace(cfg.ClientName)
	if clientName == "" {
		clientName = "What Iff"
	}
	return &Service{
		store:               store,
		httpClient:          client,
		logger:              logger,
		redirectURL:         redirectURL,
		postAuthRedirectURL: safePostAuthURL,
		allowedRedirects:    allowed,
		discoveryClient:     newDiscoveryClient(cfg.AllowLocalhost),
		allowLocalhost:      cfg.AllowLocalhost,
		clientMetadataURL:   clientMetadataURLFor(cfg.ClientMetadataURL, redirectURL),
		clientName:          clientName,
	}
}

// CallbackURL returns the redirect_uri this deployment sends to OAuth providers
// (MCP_OAUTH_REDIRECT_URL, or the localhost default).
func (s *Service) CallbackURL() string {
	return s.redirectURL
}

func (s *Service) StartAuth(ctx context.Context, userID, connectorID uuid.UUID, redirectAfter string) (string, error) {
	server, err := s.store.GetMCPServer(ctx, userID, connectorID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(server.AuthMode) != models.MCPServerAuthModeOAuth {
		return "", datastore.ErrMCPOAuthConnectorNotReady
	}
	if strings.TrimSpace(server.OAuthAuthURL) == "" || strings.TrimSpace(server.OAuthTokenURL) == "" || strings.TrimSpace(server.OAuthClientID) == "" {
		return "", datastore.ErrMCPOAuthConnectorNotReady
	}
	authURL, err := parseOAuthEndpointURL(server.OAuthAuthURL)
	if err != nil {
		return "", fmt.Errorf("invalid oauth auth url")
	}
	if _, err := parseOAuthEndpointURL(server.OAuthTokenURL); err != nil {
		return "", fmt.Errorf("invalid oauth token url")
	}

	normalizedRedirectAfter, ok := s.normalizeOptionalRedirect(redirectAfter)
	if !ok {
		return "", ErrInvalidRedirectAfter
	}

	state, err := randomToken(24)
	if err != nil {
		return "", err
	}
	codeVerifier := ""
	challenge := ""
	if pkceEnabled(server.OAuthPKCEPolicy) {
		codeVerifier, err = randomToken(32)
		if err != nil {
			return "", err
		}
		challenge = pkceS256Challenge(codeVerifier)
	}
	if _, err := s.store.CreateMCPOAuthSession(ctx, userID, connectorID, state, codeVerifier, normalizedRedirectAfter, time.Now().UTC().Add(10*time.Minute)); err != nil {
		return "", err
	}

	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", strings.TrimSpace(server.OAuthClientID))
	q.Set("redirect_uri", s.redirectURL)
	q.Set("state", state)
	if len(server.OAuthScopes) > 0 {
		q.Set("scope", strings.Join(server.OAuthScopes, " "))
	}
	if challenge != "" {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	if r := strings.TrimSpace(server.OAuthResource); r != "" {
		// RFC 8707: the token is for this MCP server, and no other.
		q.Set("resource", r)
	}
	authURL.RawQuery = q.Encode()
	return authURL.String(), nil
}

// CallbackParams are the query parameters of a provider's redirect back to the callback.
type CallbackParams struct {
	State, Code, Error, ErrorDescription string
	// Iss is the RFC 9207 issuer of the authorization response, when the provider sends one.
	Iss string
}

// CallbackResult is where to send the person after the callback, and what happened.
type CallbackResult struct {
	RedirectURL string
	Message     string
	Success     bool
	// UserID and ConnectorID are the connector that authenticated, when Success.
	UserID, ConnectorID uuid.UUID
}

func (s *Service) HandleCallback(ctx context.Context, p CallbackParams) (CallbackResult, error) {
	fail := func(redirect string, connectorID uuid.UUID, msg string) CallbackResult {
		return CallbackResult{RedirectURL: withOAuthResult(redirect, false, msg, connectorID), Message: msg}
	}
	if strings.TrimSpace(p.State) == "" {
		return fail(s.postAuthRedirectURL, uuid.Nil, "Missing OAuth state."), nil
	}
	session, server, err := s.store.ConsumeMCPOAuthSessionByState(ctx, p.State)
	if err != nil {
		return fail(s.postAuthRedirectURL, uuid.Nil, "OAuth session is invalid or expired. Start authentication again."), err
	}
	redirect := s.chooseRedirect(session.RedirectAfter)
	if issuer := strings.TrimSpace(server.OAuthIssuer); issuer != "" {
		// RFC 9207 (MCP authorization): an answer naming another issuer is not acted on, nor is its
		// error shown; one with no issuer from a server that always sends it is refused too.
		iss := strings.TrimSpace(p.Iss)
		if (iss != "" && iss != issuer) || (iss == "" && server.OAuthIssParamSupported) {
			return fail(redirect, server.ID, "The sign-in answer didn't come from this connector's sign-in server. Start authentication again."), nil
		}
	}
	if strings.TrimSpace(p.Error) != "" {
		msg := "OAuth provider declined authentication."
		if strings.TrimSpace(p.ErrorDescription) != "" {
			msg = "OAuth provider declined authentication: " + sanitizeProviderError(p.ErrorDescription)
		}
		return fail(redirect, server.ID, msg), nil
	}
	if strings.TrimSpace(p.Code) == "" {
		return fail(redirect, server.ID, "Missing OAuth code from provider callback."), nil
	}

	tokenRes, err := s.exchangeAuthorizationCode(ctx, server, p.Code, session.CodeVerifier)
	if err != nil {
		msg := "Failed to exchange OAuth authorization code."
		if markErr := s.store.MarkMCPServerOAuthRefreshFailure(ctx, session.UserID, session.MCPServerID, msg, false); markErr != nil {
			s.logger.Warn("failed to mark oauth exchange failure", zap.Error(markErr))
		}
		return fail(redirect, server.ID, msg), nil
	}
	now := time.Now().UTC()
	tokenSet := models.MCPOAuthTokenSet{
		AccessToken:     tokenRes.AccessToken,
		RefreshToken:    tokenRes.RefreshToken,
		AuthenticatedAt: &now,
		LastRefreshAt:   &now,
	}
	if tokenRes.ExpiresIn > 0 {
		expiresAt := now.Add(time.Duration(tokenRes.ExpiresIn) * time.Second)
		tokenSet.AccessTokenExpiresAt = &expiresAt
	}
	if err := s.store.SaveMCPServerOAuthTokens(ctx, session.UserID, session.MCPServerID, tokenSet); err != nil {
		return fail(redirect, server.ID, "Failed to persist OAuth tokens."), nil
	}
	msg := "Connector authentication completed."
	return CallbackResult{
		RedirectURL: withOAuthResult(redirect, true, msg, server.ID),
		Message:     msg,
		Success:     true,
		UserID:      session.UserID,
		ConnectorID: session.MCPServerID,
	}, nil
}

func (s *Service) RefreshDueConnectors(ctx context.Context, horizon time.Duration, maxFailures int) {
	if maxFailures <= 0 {
		maxFailures = 3
	}
	before := time.Now().UTC().Add(horizon)
	servers, err := s.store.ListOAuthMCPServersDueForRefresh(ctx, before, 100)
	if err != nil {
		s.logger.Warn("oauth refresh sweep list failed", zap.Error(err))
		return
	}
	for _, server := range servers {
		if server == nil {
			continue
		}
		if server.OAuthAccessTokenExpiresAt != nil && time.Until(*server.OAuthAccessTokenExpiresAt) <= 10*time.Minute {
			_ = s.store.MarkMCPServerOAuthExpiring(ctx, server.UserID, server.ID, "OAuth access token is nearing expiration.")
		}
		tokenRes, err := s.refreshToken(ctx, server)
		if err != nil {
			terminal := server.OAuthRefreshFailCount+1 >= maxFailures
			msg := "Failed to refresh OAuth access token. Reauthenticate connector."
			if markErr := s.store.MarkMCPServerOAuthRefreshFailure(ctx, server.UserID, server.ID, msg, terminal); markErr != nil {
				s.logger.Warn("mark oauth refresh failure failed", zap.Error(markErr))
			}
			continue
		}
		now := time.Now().UTC()
		set := models.MCPOAuthTokenSet{
			AccessToken:   tokenRes.AccessToken,
			RefreshToken:  server.OAuthRefreshToken,
			LastRefreshAt: &now,
		}
		if strings.TrimSpace(tokenRes.RefreshToken) != "" {
			set.RefreshToken = tokenRes.RefreshToken
		}
		if tokenRes.ExpiresIn > 0 {
			expiresAt := now.Add(time.Duration(tokenRes.ExpiresIn) * time.Second)
			set.AccessTokenExpiresAt = &expiresAt
		}
		if err := s.store.SaveMCPServerOAuthTokens(ctx, server.UserID, server.ID, set); err != nil {
			s.logger.Warn("oauth token save failed", zap.String("server_id", server.ID.String()), zap.Error(err))
		}
	}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (s *Service) exchangeAuthorizationCode(ctx context.Context, server *models.MCPServer, code, codeVerifier string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", s.redirectURL)
	form.Set("client_id", strings.TrimSpace(server.OAuthClientID))
	if strings.TrimSpace(server.OAuthClientSecret) != "" {
		form.Set("client_secret", strings.TrimSpace(server.OAuthClientSecret))
	}
	if strings.TrimSpace(codeVerifier) != "" {
		form.Set("code_verifier", strings.TrimSpace(codeVerifier))
	}
	if r := strings.TrimSpace(server.OAuthResource); r != "" {
		form.Set("resource", r)
	}
	return s.tokenRequest(ctx, server.OAuthTokenURL, form)
}

func (s *Service) refreshToken(ctx context.Context, server *models.MCPServer) (*tokenResponse, error) {
	if strings.TrimSpace(server.OAuthRefreshToken) == "" {
		return nil, fmt.Errorf("missing refresh token")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", strings.TrimSpace(server.OAuthRefreshToken))
	form.Set("client_id", strings.TrimSpace(server.OAuthClientID))
	if strings.TrimSpace(server.OAuthClientSecret) != "" {
		form.Set("client_secret", strings.TrimSpace(server.OAuthClientSecret))
	}
	if r := strings.TrimSpace(server.OAuthResource); r != "" {
		form.Set("resource", r)
	}
	return s.tokenRequest(ctx, server.OAuthTokenURL, form)
}

func (s *Service) tokenRequest(ctx context.Context, tokenURL string, form url.Values) (*tokenResponse, error) {
	parsedTokenURL, err := parseOAuthEndpointURL(tokenURL)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth token url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedTokenURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseOAuthTokenHTTPError(resp.StatusCode, body)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("oauth token endpoint returned malformed json")
	}
	if strings.TrimSpace(tr.Error) != "" {
		return nil, fmt.Errorf("oauth token error: %s", sanitizeProviderError(tr.Error))
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return nil, fmt.Errorf("oauth token endpoint returned no access_token")
	}
	return &tr, nil
}

func sanitizeProviderError(msg string) string {
	msg = oauthMessagePolicy.Sanitize(msg)
	msg = oauthControlCharsRegex.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	if len(msg) > 180 {
		return msg[:180] + "..."
	}
	return msg
}

func pkceEnabled(policy string) bool {
	switch strings.TrimSpace(policy) {
	case models.MCPServerPKCERequired, models.MCPServerPKCESupported:
		return true
	default:
		return false
	}
}

func pkceS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func withOAuthResult(base string, success bool, message string, connectorID uuid.UUID) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "http://localhost:4200/integrations"
	}
	u, err := url.Parse(base)
	if err != nil {
		u, _ = url.Parse("http://localhost:4200/integrations")
	}
	q := u.Query()
	if success {
		q.Set("oauth_status", "success")
	} else {
		q.Set("oauth_status", "error")
	}
	if connectorID != uuid.Nil {
		q.Set("connector_id", connectorID.String())
	}
	if strings.TrimSpace(message) != "" {
		q.Set("oauth_message", sanitizeProviderError(message))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func sanitizePostAuthRedirectURL(raw string) string {
	parsed, err := parseOAuthEndpointURL(raw)
	if err != nil {
		return "http://localhost:4200/integrations"
	}
	return parsed.String()
}

func sanitizeCallbackRedirectURL(raw string, logger *zap.Logger) string {
	parsed, err := parseOAuthEndpointURL(raw)
	if err != nil {
		if logger != nil {
			logger.Warn("invalid MCP_OAUTH_REDIRECT_URL, falling back to default callback URL", zap.String("redirect_url", raw))
		}
		return defaultOAuthCallbackURL
	}
	return parsed.String()
}

func parseOAuthTokenHTTPError(statusCode int, body []byte) error {
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err == nil {
		providerErr := sanitizeProviderError(tr.Error)
		providerDesc := sanitizeProviderError(tr.ErrorDesc)
		switch {
		case providerErr != "" && providerDesc != "":
			return fmt.Errorf("oauth token endpoint error (%d): %s: %s", statusCode, providerErr, providerDesc)
		case providerErr != "":
			return fmt.Errorf("oauth token endpoint error (%d): %s", statusCode, providerErr)
		case providerDesc != "":
			return fmt.Errorf("oauth token endpoint error (%d): %s", statusCode, providerDesc)
		}
		return fmt.Errorf("oauth token endpoint error (%d)", statusCode)
	}
	return fmt.Errorf("oauth token endpoint returned HTTP %d with non-JSON body", statusCode)
}

func normalizeAllowedRedirectOrigins(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, raw := range in {
		parsed, err := parseOAuthEndpointURL(raw)
		if err != nil {
			continue
		}
		out[redirectOrigin(parsed.String())] = struct{}{}
	}
	return out
}

func redirectOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func (s *Service) chooseRedirect(sessionRedirect string) string {
	if normalized, ok := s.normalizeOptionalRedirect(sessionRedirect); ok && normalized != "" {
		return normalized
	}
	return s.postAuthRedirectURL
}

func (s *Service) normalizeOptionalRedirect(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	// Allow only absolute redirects to configured origins.
	if !parsed.IsAbs() {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", false
	}
	origin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	if _, ok := s.allowedRedirects[origin]; !ok {
		return "", false
	}
	return parsed.String(), true
}

func parseOAuthEndpointURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("host is required")
	}
	return parsed, nil
}
