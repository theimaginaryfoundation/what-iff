package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/mcpoauth"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// publicDNS resolves every name to a public address, so URL checks pass without a network.
func publicDNS(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
}

func oauthRouter(provider Provider, prober ConnectionProber, oauth OAuthService) *mux.Router {
	r := mux.NewRouter()
	h := NewHandler(provider, prober, oauth, zap.NewNop(), Config{ResolveHostIPs: publicDNS})
	h.RegisterRoutes(r)
	h.RegisterPublicRoutes(r)
	return r
}

func discovered() *mcpoauth.Discovery {
	return &mcpoauth.Discovery{
		Resource:              "https://mcp.example.com/mcp",
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
		Scopes:                []string{"read"},
		IssParameterSupported: true,
	}
}

func TestDiscoverReportsTheSignInAndHowTheClientWillBeRegistered(t *testing.T) {
	oauth := &fakeOAuthService{discovery: discovered(), plan: models.MCPOAuthRegistrationMetadataDocument}
	rr := httptest.NewRecorder()
	oauthRouter(&fakeProvider{}, &fakeProber{}, oauth).ServeHTTP(rr,
		newAuthedRequest(t, http.MethodPost, "/mcp-servers/oauth/discover", []byte(`{"server_url":"https://mcp.example.com/mcp"}`)))

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "https://auth.example.com", body["issuer"])
	assert.Equal(t, "metadata_document", body["registration"])
	assert.Equal(t, "https://mcp.example.com/mcp", oauth.lastDiscoverURL)
}

func TestDiscoverAnswersAStableCodePerFailure(t *testing.T) {
	for err, code := range map[error]string{
		mcpoauth.ErrNoAuthRequired:                                    models.ErrCodeMCPOAuthNotRequired,
		mcpoauth.ErrClientRegistrationUnavailable:                     models.ErrCodeMCPOAuthClientIDRequired,
		errors.Join(mcpoauth.ErrDiscovery, errors.New("no metadata")): models.ErrCodeMCPOAuthDiscoveryFailed,
	} {
		rr := httptest.NewRecorder()
		oauthRouter(&fakeProvider{}, &fakeProber{}, &fakeOAuthService{discoverErr: err}).ServeHTTP(rr,
			newAuthedRequest(t, http.MethodPost, "/mcp-servers/oauth/discover", []byte(`{"server_url":"https://mcp.example.com/mcp"}`)))
		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
		assert.Contains(t, rr.Body.String(), `"`+code+`"`)
	}
}

func TestDiscoverRefusesAPrivateServerURL(t *testing.T) {
	oauth := &fakeOAuthService{discovery: discovered()}
	rr := httptest.NewRecorder()
	oauthRouter(&fakeProvider{}, &fakeProber{}, oauth).ServeHTTP(rr,
		newAuthedRequest(t, http.MethodPost, "/mcp-servers/oauth/discover", []byte(`{"server_url":"https://10.0.0.5/mcp"}`)))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Empty(t, oauth.lastDiscoverURL, "nothing is fetched")
}

func TestCreateWithOAuthAutoNeedsOnlyTheServerURL(t *testing.T) {
	provider := &fakeProvider{}
	oauth := &fakeOAuthService{discovery: discovered(), registration: mcpoauth.Registration{
		Method: models.MCPOAuthRegistrationDynamic, ClientID: "dyn-1", ClientSecret: "shh",
	}}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPost, "/mcp-servers", []byte(`{
		"name":"Files","description":"My files","server_url":"https://mcp.example.com/mcp",
		"auth_mode":"oauth","oauth_auto":true
	}`)))

	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	require.NotNil(t, provider.created)
	c := provider.created
	assert.Equal(t, "https://auth.example.com/authorize", c.OAuthAuthURL)
	assert.Equal(t, "https://auth.example.com/token", c.OAuthTokenURL)
	assert.Equal(t, "dyn-1", c.OAuthClientID)
	assert.Equal(t, "shh", c.OAuthClientSecret)
	assert.Equal(t, models.MCPServerPKCERequired, c.OAuthPKCEPolicy)
	assert.Equal(t, "https://mcp.example.com/mcp", c.OAuthResource)
	assert.Equal(t, "https://auth.example.com", c.OAuthIssuer)
	assert.True(t, c.OAuthIssParamSupported)
	assert.Equal(t, models.MCPOAuthRegistrationDynamic, c.OAuthClientRegistration)
	assert.NotContains(t, rr.Body.String(), "shh", "the secret is never answered")
}

func TestCreateWithOAuthAutoKeepsAGivenClientID(t *testing.T) {
	provider := &fakeProvider{}
	oauth := &fakeOAuthService{discovery: discovered()}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPost, "/mcp-servers", []byte(`{
		"name":"Files","description":"My files","server_url":"https://mcp.example.com/mcp",
		"auth_mode":"oauth","oauth_auto":true,"oauth_client_id":"mine","oauth_client_secret":"s"
	}`)))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	assert.Equal(t, "mine", oauth.lastAutoClientID)
	assert.Equal(t, "mine", provider.created.OAuthClientID)
	assert.Equal(t, models.MCPOAuthRegistrationManual, provider.created.OAuthClientRegistration)
}

func TestCreateWithOAuthAutoReportsAFailedDiscovery(t *testing.T) {
	provider := &fakeProvider{}
	oauth := &fakeOAuthService{discoverErr: mcpoauth.ErrClientRegistrationUnavailable}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPost, "/mcp-servers", []byte(`{
		"name":"Files","description":"My files","server_url":"https://mcp.example.com/mcp",
		"auth_mode":"oauth","oauth_auto":true
	}`)))
	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	assert.Contains(t, rr.Body.String(), models.ErrCodeMCPOAuthClientIDRequired)
	assert.Nil(t, provider.created, "nothing is saved")
}

func discoveredConnector(id uuid.UUID) *models.MCPServer {
	return &models.MCPServer{
		ID: id, Name: "Files", Description: "My files", ServerURL: "https://mcp.example.com/mcp",
		AuthMode: models.MCPServerAuthModeOAuth, OAuthAuthURL: "https://auth.example.com/authorize",
		OAuthTokenURL: "https://auth.example.com/token", OAuthClientID: "dyn-1", OAuthPKCEPolicy: models.MCPServerPKCERequired,
		OAuthResource: "https://mcp.example.com/mcp", OAuthIssuer: "https://auth.example.com",
		OAuthClientRegistration: models.MCPOAuthRegistrationDynamic,
	}
}

func TestMovingADiscoveredConnectorDiscoversItsNewServer(t *testing.T) {
	id := uuid.New()
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		return discoveredConnector(id), nil
	}}
	d := discovered()
	d.Resource, d.Issuer = "https://new.example.com/mcp", "https://new-auth.example.com"
	oauth := &fakeOAuthService{discovery: d, registration: mcpoauth.Registration{Method: models.MCPOAuthRegistrationMetadataDocument, ClientID: "https://app.example.com/client.json"}}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPatch, "/mcp-servers/"+id.String(),
		[]byte(`{"server_url":"https://new.example.com/mcp"}`)))

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "https://new.example.com/mcp", oauth.lastDiscoverURL)
	assert.Equal(t, "https://new-auth.example.com", provider.updated.OAuthIssuer)
	assert.Equal(t, "https://app.example.com/client.json", provider.updated.OAuthClientID)
	assert.True(t, provider.secretUpdate.Provided && provider.secretUpdate.Clear, "the old client's secret goes")
}

func TestEditingADiscoveredConnectorByHandMakesItManual(t *testing.T) {
	id := uuid.New()
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		return discoveredConnector(id), nil
	}}
	oauth := &fakeOAuthService{}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPatch, "/mcp-servers/"+id.String(),
		[]byte(`{"oauth_token_url":"https://other.example.com/token"}`)))

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Empty(t, oauth.lastDiscoverURL)
	assert.Empty(t, provider.updated.OAuthIssuer)
	assert.Empty(t, provider.updated.OAuthResource)
	assert.Equal(t, models.MCPOAuthRegistrationManual, provider.updated.OAuthClientRegistration)
}

func TestRenamingADiscoveredConnectorKeepsItsSignIn(t *testing.T) {
	id := uuid.New()
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		return discoveredConnector(id), nil
	}}
	oauth := &fakeOAuthService{}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPatch, "/mcp-servers/"+id.String(),
		[]byte(`{"name":"Renamed"}`)))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Empty(t, oauth.lastDiscoverURL)
	assert.Equal(t, "https://auth.example.com", provider.updated.OAuthIssuer)
	assert.False(t, provider.secretUpdate.Provided)
}

func TestTheCallbackPassesIssAndListsToolsAfterSignIn(t *testing.T) {
	userID, connectorID := uuid.New(), uuid.New()
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		s := discoveredConnector(connectorID)
		s.OAuthAccessToken = "at"
		return s, nil
	}}
	prober := &fakeProber{toolCount: 7}
	oauth := &fakeOAuthService{callbackURL: "http://localhost:4200/integrations?oauth_status=success", callbackUserID: userID, callbackConnectorID: connectorID}
	rr := httptest.NewRecorder()
	oauthRouter(provider, prober, oauth).ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
		"/mcp-servers/oauth/callback?state=s&code=c&iss=https%3A%2F%2Fauth.example.com", nil))

	require.Equal(t, http.StatusFound, rr.Code)
	assert.Equal(t, "https://auth.example.com", oauth.lastCallback.Iss)
	require.Equal(t, 1, prober.calls)
	assert.Equal(t, "at", prober.last.OAuthAccessToken)
	require.Len(t, provider.runtimeUpdates, 1)
	assert.Equal(t, 7, provider.runtimeUpdates[0].ToolCount)
	assert.Equal(t, models.MCPServerStatusActive, provider.runtimeUpdates[0].Status)
}

func TestAFailedToolListingAfterSignInIsReportedNotFatal(t *testing.T) {
	userID, connectorID := uuid.New(), uuid.New()
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		return discoveredConnector(connectorID), nil
	}}
	prober := &fakeProber{err: errors.New("403 insufficient_scope")}
	oauth := &fakeOAuthService{callbackURL: "http://localhost:4200/integrations?oauth_status=success", callbackUserID: userID, callbackConnectorID: connectorID}
	rr := httptest.NewRecorder()
	oauthRouter(provider, prober, oauth).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/mcp-servers/oauth/callback?state=s&code=c", nil))

	require.Equal(t, http.StatusFound, rr.Code, "the person is still sent back")
	require.Len(t, provider.runtimeUpdates, 1)
	assert.Contains(t, provider.runtimeUpdates[0].Reason, "insufficient_scope")
}

func TestASignedInOAuthConnectorCanBeTested(t *testing.T) {
	connectorID := uuid.New()
	expires := time.Now().Add(time.Hour)
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		s := discoveredConnector(connectorID)
		s.OAuthAccessToken, s.OAuthAccessTokenExpiresAt = "at", &expires
		return s, nil
	}}
	prober := &fakeProber{toolCount: 4}
	rr := httptest.NewRecorder()
	oauthRouter(provider, prober, &fakeOAuthService{}).ServeHTTP(rr, newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection",
		[]byte(`{"server_url":"https://mcp.example.com/mcp","connector_id":"`+connectorID.String()+`"}`)))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"pass":true`)
	assert.Equal(t, "at", prober.last.OAuthAccessToken)
	assert.Equal(t, &expires, prober.last.OAuthAccessTokenExpiresAt)
}

func TestClientMetadataDocumentIsServedPublicly(t *testing.T) {
	oauth := &fakeOAuthService{metadataDoc: map[string]string{"client_id": "https://app.example.com/api/mcp-servers/oauth/client-metadata.json"}}
	rr := httptest.NewRecorder()
	oauthRouter(&fakeProvider{}, &fakeProber{}, oauth).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/mcp-servers/oauth/client-metadata.json", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "client-metadata.json")

	rr = httptest.NewRecorder()
	oauthRouter(&fakeProvider{}, &fakeProber{}, &fakeOAuthService{}).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/mcp-servers/oauth/client-metadata.json", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code, "no document without a public https address")
}

func TestCheckingAgainWithTheSameOwnClientKeepsItsSecret(t *testing.T) {
	id := uuid.New()
	provider := &fakeProvider{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.MCPServer, error) {
		s := discoveredConnector(id)
		s.OAuthClientID, s.OAuthClientRegistration = "mine", models.MCPOAuthRegistrationManual
		return s, nil
	}}
	oauth := &fakeOAuthService{discovery: discovered()}
	rr := httptest.NewRecorder()
	oauthRouter(provider, &fakeProber{}, oauth).ServeHTTP(rr, newAuthedRequest(t, http.MethodPatch, "/mcp-servers/"+id.String(),
		[]byte(`{"oauth_auto":true,"oauth_client_id":"mine"}`)))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "mine", oauth.lastAutoClientID)
	assert.False(t, provider.secretUpdate.Provided, "the stored secret is kept")
}

func TestADiscoveryFailureReadsAsASentence(t *testing.T) {
	rr := httptest.NewRecorder()
	oauthRouter(&fakeProvider{}, &fakeProber{}, &fakeOAuthService{discoverErr: fmt.Errorf("%w: no metadata", mcpoauth.ErrDiscovery)}).ServeHTTP(rr,
		newAuthedRequest(t, http.MethodPost, "/mcp-servers/oauth/discover", []byte(`{"server_url":"https://mcp.example.com/mcp"}`)))
	assert.Contains(t, rr.Body.String(), `"Couldn't read this server's sign-in settings: no metadata."`)
}
