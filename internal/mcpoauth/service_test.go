package mcpoauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type fakeStore struct {
	server          *models.MCPServer
	createdState    string
	createdVerifier string
	createdRedirect string
	consumeRedirect string
	savedTokenSet   *models.MCPOAuthTokenSet
}

func (f *fakeStore) GetMCPServer(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
	return f.server, nil
}
func (f *fakeStore) CreateMCPOAuthSession(_ context.Context, _ uuid.UUID, _ uuid.UUID, state, codeVerifier, redirectAfter string, _ time.Time) (*models.MCPOAuthSession, error) {
	f.createdState = state
	f.createdVerifier = codeVerifier
	f.createdRedirect = redirectAfter
	return &models.MCPOAuthSession{State: state, CodeVerifier: codeVerifier}, nil
}
func (f *fakeStore) ConsumeMCPOAuthSessionByState(context.Context, string) (*models.MCPOAuthSession, *models.MCPServer, error) {
	return &models.MCPOAuthSession{
		UserID:        f.server.UserID,
		MCPServerID:   f.server.ID,
		State:         "state",
		CodeVerifier:  f.createdVerifier,
		RedirectAfter: f.consumeRedirect,
	}, f.server, nil
}
func (f *fakeStore) SaveMCPServerOAuthTokens(_ context.Context, _, _ uuid.UUID, set models.MCPOAuthTokenSet) error {
	copy := set
	f.savedTokenSet = &copy
	return nil
}
func (f *fakeStore) MarkMCPServerOAuthRefreshFailure(context.Context, uuid.UUID, uuid.UUID, string, bool) error {
	return nil
}
func (f *fakeStore) ListOAuthMCPServersDueForRefresh(context.Context, time.Time, int) ([]*models.MCPServer, error) {
	return nil, nil
}
func (f *fakeStore) MarkMCPServerOAuthExpiring(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func TestStartAuthBuildsAuthorizeURL(t *testing.T) {
	store := &fakeStore{
		server: &models.MCPServer{
			ID:              uuid.New(),
			UserID:          uuid.New(),
			AuthMode:        models.MCPServerAuthModeOAuth,
			OAuthAuthURL:    "https://accounts.example.com/auth",
			OAuthTokenURL:   "https://accounts.example.com/token",
			OAuthClientID:   "client-id",
			OAuthScopes:     []string{"openid", "email"},
			OAuthPKCEPolicy: models.MCPServerPKCERequired,
		},
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:      "http://localhost:8080/api/mcp-servers/oauth/callback",
		AllowedRedirects: []string{"http://localhost:4200"},
	})
	authURL, err := svc.StartAuth(context.Background(), store.server.UserID, store.server.ID, "")
	require.NoError(t, err)
	require.Contains(t, authURL, "response_type=code")
	require.Contains(t, authURL, "client_id=client-id")
	require.NotEmpty(t, store.createdState)
	require.NotEmpty(t, store.createdVerifier)
}

func TestHandleCallbackExchangesAndStoresTokens(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	store := &fakeStore{
		server: &models.MCPServer{
			ID:                uuid.New(),
			UserID:            uuid.New(),
			AuthMode:          models.MCPServerAuthModeOAuth,
			OAuthTokenURL:     tokenSrv.URL,
			OAuthClientID:     "client-id",
			OAuthClientSecret: "secret",
		},
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:         "http://localhost:8080/api/mcp-servers/oauth/callback",
		PostAuthRedirectURL: "http://localhost:4200/integrations",
		AllowedRedirects:    []string{"http://localhost:4200"},
	})
	store.createdVerifier = "verifier"
	redirectURL, _, success, err := svc.HandleCallback(context.Background(), "state", "auth-code", "", "")
	require.NoError(t, err)
	require.True(t, success)
	require.Contains(t, redirectURL, "oauth_status=success")
	require.NotNil(t, store.savedTokenSet)
	require.Equal(t, "at", store.savedTokenSet.AccessToken)
	require.Equal(t, "rt", store.savedTokenSet.RefreshToken)
}

func TestStartAuthRejectsNonOAuthConnector(t *testing.T) {
	store := &fakeStore{
		server: &models.MCPServer{
			ID:       uuid.New(),
			UserID:   uuid.New(),
			AuthMode: models.MCPServerAuthModeHeader,
		},
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:      "http://localhost:8080/api/mcp-servers/oauth/callback",
		AllowedRedirects: []string{"http://localhost:4200"},
	})
	_, err := svc.StartAuth(context.Background(), store.server.UserID, store.server.ID, "")
	require.ErrorIs(t, err, datastore.ErrMCPOAuthConnectorNotReady)
}

func TestStartAuthRejectsInvalidURLs(t *testing.T) {
	store := &fakeStore{
		server: &models.MCPServer{
			ID:            uuid.New(),
			UserID:        uuid.New(),
			AuthMode:      models.MCPServerAuthModeOAuth,
			OAuthAuthURL:  "not-a-url",
			OAuthTokenURL: "https://oauth.example.com/token",
			OAuthClientID: "client-id",
		},
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:      "http://localhost:8080/api/mcp-servers/oauth/callback",
		AllowedRedirects: []string{"http://localhost:4200"},
	})
	_, err := svc.StartAuth(context.Background(), store.server.UserID, store.server.ID, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid oauth auth url")
}

func TestHandleCallbackFailsOnMalformedTokenJSON(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer tokenSrv.Close()

	store := &fakeStore{
		server: &models.MCPServer{
			ID:                uuid.New(),
			UserID:            uuid.New(),
			AuthMode:          models.MCPServerAuthModeOAuth,
			OAuthTokenURL:     tokenSrv.URL,
			OAuthClientID:     "client-id",
			OAuthClientSecret: "secret",
		},
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:         "http://localhost:8080/api/mcp-servers/oauth/callback",
		PostAuthRedirectURL: "http://localhost:4200/integrations",
		AllowedRedirects:    []string{"http://localhost:4200"},
	})
	store.createdVerifier = "verifier"
	redirectURL, message, success, err := svc.HandleCallback(context.Background(), "state", "auth-code", "", "")
	require.NoError(t, err)
	require.False(t, success)
	require.Contains(t, message, "Failed to exchange OAuth authorization code")
	require.Contains(t, redirectURL, "oauth_status=error")
	require.Nil(t, store.savedTokenSet)
}

func TestWithOAuthResult_SanitizesOAuthMessage(t *testing.T) {
	redirect := withOAuthResult("http://localhost:4200/integrations", false, `<script>alert("xss")</script> oauth <b>error</b>`, uuid.New())
	parsed, err := url.Parse(redirect)
	require.NoError(t, err)
	raw := parsed.Query().Get("oauth_message")
	require.NotContains(t, strings.ToLower(raw), "<script")
	require.NotContains(t, strings.ToLower(raw), "</script")
	require.Equal(t, "oauth error", raw)
}

func TestStartAuthRejectsDisallowedRedirectAfter(t *testing.T) {
	store := &fakeStore{
		server: &models.MCPServer{
			ID:            uuid.New(),
			UserID:        uuid.New(),
			AuthMode:      models.MCPServerAuthModeOAuth,
			OAuthAuthURL:  "https://accounts.example.com/auth",
			OAuthTokenURL: "https://oauth.example.com/token",
			OAuthClientID: "client-id",
		},
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:         "http://localhost:8080/api/mcp-servers/oauth/callback",
		PostAuthRedirectURL: "http://localhost:4200/integrations",
		AllowedRedirects:    []string{"http://localhost:4200"},
	})
	_, err := svc.StartAuth(context.Background(), store.server.UserID, store.server.ID, "https://evil.example/steal")
	require.ErrorIs(t, err, ErrInvalidRedirectAfter)
}

func TestHandleCallbackFallsBackWhenSessionRedirectNotAllowed(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	store := &fakeStore{
		server: &models.MCPServer{
			ID:                uuid.New(),
			UserID:            uuid.New(),
			AuthMode:          models.MCPServerAuthModeOAuth,
			OAuthTokenURL:     tokenSrv.URL,
			OAuthClientID:     "client-id",
			OAuthClientSecret: "secret",
		},
		consumeRedirect: "https://evil.example/capture",
	}
	svc := New(store, nil, nil, Config{
		RedirectURL:         "http://localhost:8080/api/mcp-servers/oauth/callback",
		PostAuthRedirectURL: "http://localhost:4200/integrations",
		AllowedRedirects:    []string{"http://localhost:4200"},
	})
	store.createdVerifier = "verifier"
	redirectURL, _, success, err := svc.HandleCallback(context.Background(), "state", "auth-code", "", "")
	require.NoError(t, err)
	require.True(t, success)
	require.True(t, strings.HasPrefix(redirectURL, "http://localhost:4200/integrations"))
}
