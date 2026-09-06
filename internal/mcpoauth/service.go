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
}

type Config struct {
	RedirectURL         string
	PostAuthRedirectURL string
	AllowedRedirects    []string
}

var (
	oauthMessagePolicy      = bluemonday.StrictPolicy()
	oauthControlCharsRegex  = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
	ErrInvalidRedirectAfter = errors.New("invalid redirect_after")
)

func New(store Store, client *http.Client, logger *zap.Logger, cfg Config) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	safePostAuthURL := sanitizePostAuthRedirectURL(strings.TrimSpace(cfg.PostAuthRedirectURL))
	allowed := normalizeAllowedRedirectOrigins(cfg.AllowedRedirects)
	allowed[redirectOrigin(safePostAuthURL)] = struct{}{}

	return &Service{
		store:               store,
		httpClient:          client,
		logger:              logger,
		redirectURL:         strings.TrimSpace(cfg.RedirectURL),
		postAuthRedirectURL: safePostAuthURL,
		allowedRedirects:    allowed,
	}
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
	authURL.RawQuery = q.Encode()
	return authURL.String(), nil
}

func (s *Service) HandleCallback(ctx context.Context, state, code, oauthErr, oauthErrDesc string) (redirectURL, message string, success bool, err error) {
	safeFail := func(msg string) (string, string, bool, error) {
		return withOAuthResult(s.postAuthRedirectURL, false, msg, uuid.Nil), msg, false, nil
	}
	if strings.TrimSpace(state) == "" {
		return safeFail("Missing OAuth state.")
	}
	session, server, err := s.store.ConsumeMCPOAuthSessionByState(ctx, state)
	if err != nil {
		msg := "OAuth session is invalid or expired. Start authentication again."
		redirectURL, message, success, _ := safeFail(msg)
		if errors.Is(err, datastore.ErrMCPOAuthSessionNotFound) ||
			errors.Is(err, datastore.ErrMCPOAuthSessionExpired) ||
			errors.Is(err, datastore.ErrMCPOAuthSessionConsumed) {
			return redirectURL, message, success, err
		}
		return redirectURL, message, success, err
	}
	if strings.TrimSpace(oauthErr) != "" {
		msg := "OAuth provider declined authentication."
		if strings.TrimSpace(oauthErrDesc) != "" {
			msg = "OAuth provider declined authentication: " + sanitizeProviderError(oauthErrDesc)
		}
		return withOAuthResult(s.chooseRedirect(session.RedirectAfter), false, msg, server.ID), msg, false, nil
	}
	if strings.TrimSpace(code) == "" {
		msg := "Missing OAuth code from provider callback."
		return withOAuthResult(s.chooseRedirect(session.RedirectAfter), false, msg, server.ID), msg, false, nil
	}

	tokenRes, err := s.exchangeAuthorizationCode(ctx, server, code, session.CodeVerifier)
	if err != nil {
		msg := "Failed to exchange OAuth authorization code."
		if markErr := s.store.MarkMCPServerOAuthRefreshFailure(ctx, session.UserID, session.MCPServerID, msg, false); markErr != nil {
			s.logger.Warn("failed to mark oauth exchange failure", zap.Error(markErr))
		}
		return withOAuthResult(s.chooseRedirect(session.RedirectAfter), false, msg, server.ID), msg, false, nil
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
		msg := "Failed to persist OAuth tokens."
		return withOAuthResult(s.chooseRedirect(session.RedirectAfter), false, msg, server.ID), msg, false, nil
	}
	msg := "Connector authentication completed."
	return withOAuthResult(s.chooseRedirect(session.RedirectAfter), true, msg, server.ID), msg, true, nil
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
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("oauth token endpoint returned malformed json")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth token endpoint error (%d)", resp.StatusCode)
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
