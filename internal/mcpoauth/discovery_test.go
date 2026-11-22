package mcpoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// fakeMCP is an MCP server and its sign-in server on one httptest server: routes are added
// per test, and every request's path is recorded in order.
type fakeMCP struct {
	*httptest.Server
	mu     sync.Mutex
	seen   []string
	routes map[string]http.HandlerFunc
}

func newFakeMCP(t *testing.T) *fakeMCP {
	f := &fakeMCP{routes: map[string]http.HandlerFunc{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Method+" "+r.URL.Path)
		h := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMCP) handle(method, path string, h http.HandlerFunc) { f.routes[method+" "+path] = h }

func (f *fakeMCP) json(method, path string, v any) {
	f.handle(method, path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	})
}

func (f *fakeMCP) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func unauthorized(challenge string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}
}

func asMetadata(issuer string) map[string]any {
	return map[string]any{
		"issuer":                           issuer,
		"authorization_endpoint":           issuer + "/authorize",
		"token_endpoint":                   issuer + "/token",
		"code_challenge_methods_supported": []string{"S256"},
	}
}

// localService is a Service whose discovery may reach the loopback test servers, as a
// deployment with MCP_ALLOW_LOCALHOST_CONNECTIONS does.
func localService(cfg Config) *Service {
	cfg.AllowLocalhost = true
	if cfg.RedirectURL == "" {
		cfg.RedirectURL = "https://app.example.com/api/mcp-servers/oauth/callback"
	}
	return New(&fakeStore{}, nil, nil, cfg)
}

func TestDiscoverFollowsTheChallengeToTheAuthorizationServer(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(`Bearer realm="mcp", resource_metadata="`+f.URL+`/meta/prm", scope="files:read files:write"`))
	f.json("GET", "/meta/prm", map[string]any{
		"resource":              f.URL + "/mcp/",
		"authorization_servers": []string{f.URL + "/tenant"},
		"scopes_supported":      []string{"everything"},
		"resource_name":         "Files",
	})
	as := asMetadata(f.URL + "/tenant")
	as["registration_endpoint"] = f.URL + "/tenant/register"
	as["client_id_metadata_document_supported"] = true
	as["authorization_response_iss_parameter_supported"] = true
	as["scopes_supported"] = []string{"files:read", "files:write", "offline_access"}
	f.json("GET", "/.well-known/oauth-authorization-server/tenant", as)

	d, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.NoError(t, err)
	assert.Equal(t, f.URL+"/mcp", d.Resource, "the canonical resource has no trailing slash")
	assert.Equal(t, "Files", d.ResourceName)
	assert.Equal(t, f.URL+"/tenant", d.Issuer)
	assert.Equal(t, f.URL+"/tenant/authorize", d.AuthorizationEndpoint)
	assert.Equal(t, f.URL+"/tenant/token", d.TokenEndpoint)
	assert.Equal(t, f.URL+"/tenant/register", d.RegistrationEndpoint)
	assert.Equal(t, []string{"files:read", "files:write", "offline_access"}, d.Scopes,
		"the challenge's scopes win over scopes_supported, plus offline_access the server offers")
	assert.True(t, d.ClientIDMetadataDocumentSupported)
	assert.True(t, d.IssParameterSupported)
	assert.False(t, d.Legacy)
}

func TestDiscoverTriesTheWellKnownAddressesInTheSpecsOrder(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/public/mcp", unauthorized(`Bearer error="invalid_token"`))
	f.json("GET", "/.well-known/oauth-protected-resource", map[string]any{
		"authorization_servers": []string{f.URL},
		"scopes_supported":      []string{"read"},
	})
	f.json("GET", "/.well-known/openid-configuration", asMetadata(f.URL))

	d, err := localService(Config{}).Discover(context.Background(), f.URL+"/public/mcp")
	require.NoError(t, err)
	assert.Equal(t, []string{"read"}, d.Scopes)
	assert.Equal(t, f.URL+"/public/mcp", d.Resource)
	assert.Equal(t, []string{
		"POST /public/mcp",
		"GET /.well-known/oauth-protected-resource/public/mcp", // path-inserted first
		"GET /.well-known/oauth-protected-resource",            // then the root
		"GET /.well-known/oauth-authorization-server",          // RFC 8414 first
		"GET /.well-known/openid-configuration",                // then OpenID Connect
	}, f.requests())
}

func TestDiscoverTriesAllThreeAddressesForAnIssuerWithAPath(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(""))
	f.json("GET", "/.well-known/oauth-protected-resource/mcp", map[string]any{"authorization_servers": []string{f.URL + "/realms/x"}})
	f.json("GET", "/realms/x/.well-known/openid-configuration", asMetadata(f.URL+"/realms/x"))

	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"POST /mcp",
		"GET /.well-known/oauth-protected-resource/mcp",
		"GET /.well-known/oauth-authorization-server/realms/x",
		"GET /.well-known/openid-configuration/realms/x",
		"GET /realms/x/.well-known/openid-configuration",
	}, f.requests())
}

func TestDiscoverRefusesMetadataNamingAnotherIssuer(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(""))
	f.json("GET", "/.well-known/oauth-protected-resource/mcp", map[string]any{"authorization_servers": []string{f.URL}})
	f.json("GET", "/.well-known/oauth-authorization-server", asMetadata("https://honest.example"))

	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "names another issuer")
}

func TestDiscoverRefusesAServerWithoutPKCES256(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(""))
	f.json("GET", "/.well-known/oauth-protected-resource/mcp", map[string]any{"authorization_servers": []string{f.URL}})
	as := asMetadata(f.URL)
	delete(as, "code_challenge_methods_supported")
	f.json("GET", "/.well-known/oauth-authorization-server", as)

	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "PKCE")
}

func TestDiscoverRefusesMetadataForAnotherResource(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(""))
	f.json("GET", "/.well-known/oauth-protected-resource/mcp", map[string]any{
		"resource":              "https://elsewhere.example/mcp",
		"authorization_servers": []string{f.URL},
	})
	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "another resource")
}

func TestDiscoverReadsALegacyServerAsItsOwnAuthorizationServer(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(`Bearer`))
	f.json("GET", "/.well-known/oauth-authorization-server", asMetadata(f.URL))

	d, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.NoError(t, err)
	assert.True(t, d.Legacy)
	assert.Equal(t, f.URL, d.Issuer)
	assert.Equal(t, f.URL+"/mcp", d.Resource)
}

func TestDiscoverSaysWhenNoSignInIsNeeded(t *testing.T) {
	f := newFakeMCP(t)
	f.json("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})

	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.ErrorIs(t, err, ErrNoAuthRequired)
}

func TestDiscoverSaysWhenAnAskingServerPublishesNoMetadata(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(""))

	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "enter its OAuth settings by hand")
}

func TestDiscoverStaysOffInternalAddresses(t *testing.T) {
	svc := New(&fakeStore{}, nil, nil, Config{}) // localhost not allowed

	_, err := svc.Discover(context.Background(), "http://127.0.0.1:9/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "https")

	_, err = svc.Discover(context.Background(), "https://127.0.0.1:9/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "non-public address")
}

func TestDiscoveryNeverFollowsMetadataToInternalAddresses(t *testing.T) {
	// The MCP server is reachable (allowed loopback, as in local development), but nothing it
	// names may send discovery over plain http to a host that isn't loopback.
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(`Bearer resource_metadata="http://169.254.169.254/latest/meta-data"`))
	_, err := localService(Config{}).Discover(context.Background(), f.URL+"/mcp")
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "only https")
}

func TestBearerChallengeParams(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   map[string]string
		ok     bool
	}{
		{`Bearer resource_metadata="https://a.example/prm", scope="a b"`, map[string]string{"resource_metadata": "https://a.example/prm", "scope": "a b"}, true},
		{`Basic realm="x", Bearer error="invalid_token", resource_metadata="u"`, map[string]string{"error": "invalid_token", "resource_metadata": "u"}, true},
		{`bearer scope=read`, map[string]string{"scope": "read"}, true},
		{`Bearer scope="a", DPoP algs="ES256"`, map[string]string{"scope": "a"}, true},
		{`Bearer realm="say \"hi\""`, map[string]string{"realm": `say "hi"`}, true},
		{`Bearer`, map[string]string{}, true},
		{`Basic realm="x"`, nil, false},
		{`NotBearer x="y"`, nil, false},
	} {
		got, ok := bearerChallengeParams(tc.header)
		assert.Equal(t, tc.ok, ok, tc.header)
		if tc.ok {
			assert.Equal(t, tc.want, got, tc.header)
		}
	}
}

func TestCanonicalResource(t *testing.T) {
	for in, want := range map[string]string{
		"HTTPS://MCP.Example.com/mcp/":     "https://mcp.example.com/mcp",
		"https://mcp.example.com":          "https://mcp.example.com",
		"https://mcp.example.com:8443/":    "https://mcp.example.com:8443",
		"https://mcp.example.com/mcp#frag": "https://mcp.example.com/mcp",
		"https://mcp.example.com/mcp?x=1":  "https://mcp.example.com/mcp",
	} {
		got, err := CanonicalResource(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := CanonicalResource("mcp.example.com")
	assert.Error(t, err)
}

func TestClientMetadataURLSitsBesideAnHTTPSCallback(t *testing.T) {
	assert.Equal(t, "https://app.example.com/api/mcp-servers/oauth/client-metadata.json",
		clientMetadataURLFor("", "https://app.example.com/api/mcp-servers/oauth/callback"))
	assert.Empty(t, clientMetadataURLFor("", "http://localhost:8080/api/mcp-servers/oauth/callback"), "a client ID must be an https URL")
	assert.Empty(t, clientMetadataURLFor("", "https://app.example.com/somewhere-else"))
	assert.Equal(t, "https://cdn.example.com/client.json", clientMetadataURLFor("https://cdn.example.com/client.json", "http://localhost/x"))
	assert.Empty(t, clientMetadataURLFor("https://cdn.example.com", ""), "a client ID URL needs a path")
}

func TestClientMetadataDocumentNamesItsOwnURLAndTheCallback(t *testing.T) {
	svc := localService(Config{})
	doc, ok := svc.ClientMetadataDocument()
	require.True(t, ok)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "https://app.example.com/api/mcp-servers/oauth/client-metadata.json", m["client_id"])
	assert.Equal(t, []any{"https://app.example.com/api/mcp-servers/oauth/callback"}, m["redirect_uris"])
	assert.Equal(t, "What Iff", m["client_name"])
	assert.Equal(t, "none", m["token_endpoint_auth_method"])
	assert.Equal(t, "web", m["application_type"])

	_, ok = New(&fakeStore{}, nil, nil, Config{}).ClientMetadataDocument()
	assert.False(t, ok, "no https callback, no document")
}

func TestRegisterPrefersAMetadataDocumentThenDynamicRegistration(t *testing.T) {
	f := newFakeMCP(t)
	var body map[string]any
	f.handle("POST", "/register", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"dyn-123","client_secret":"shh"}`))
	})
	svc := localService(Config{})

	reg, err := svc.Register(context.Background(), &Discovery{ClientIDMetadataDocumentSupported: true, RegistrationEndpoint: f.URL + "/register"})
	require.NoError(t, err)
	assert.Equal(t, Registration{Method: models.MCPOAuthRegistrationMetadataDocument, ClientID: svc.ClientMetadataURL()}, reg)
	assert.Empty(t, f.requests(), "a metadata document registers nothing")

	reg, err = svc.Register(context.Background(), &Discovery{RegistrationEndpoint: f.URL + "/register"})
	require.NoError(t, err)
	assert.Equal(t, Registration{Method: models.MCPOAuthRegistrationDynamic, ClientID: "dyn-123", ClientSecret: "shh"}, reg)
	assert.Equal(t, []any{"https://app.example.com/api/mcp-servers/oauth/callback"}, body["redirect_uris"])
	assert.Equal(t, "none", body["token_endpoint_auth_method"])
	assert.Equal(t, "web", body["application_type"])

	_, err = svc.Register(context.Background(), &Discovery{RegistrationEndpoint: f.URL + "/register", TokenEndpointAuthMethods: []string{"client_secret_basic", "client_secret_post"}})
	require.NoError(t, err)
	assert.Equal(t, "client_secret_post", body["token_endpoint_auth_method"], "a server without public clients gets a secret sent in the form")

	_, err = svc.Register(context.Background(), &Discovery{})
	require.ErrorIs(t, err, ErrClientRegistrationUnavailable)
}

func TestRegisterWithoutAnHTTPSCallbackFallsBackToDynamicRegistration(t *testing.T) {
	f := newFakeMCP(t)
	f.json("POST", "/register", map[string]any{"client_id": "dyn"})
	svc := New(&fakeStore{}, nil, nil, Config{AllowLocalhost: true}) // localhost callback: no metadata document
	reg, err := svc.Register(context.Background(), &Discovery{ClientIDMetadataDocumentSupported: true, RegistrationEndpoint: f.URL + "/register"})
	require.NoError(t, err)
	assert.Equal(t, models.MCPOAuthRegistrationDynamic, reg.Method)
}

func TestRegisterReportsARefusal(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/register", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri","error_description":"redirect not allowed"}`))
	})
	_, err := localService(Config{}).Register(context.Background(), &Discovery{RegistrationEndpoint: f.URL + "/register"})
	require.ErrorIs(t, err, ErrDiscovery)
	assert.Contains(t, err.Error(), "redirect not allowed")
}

func TestAutoConfigureUsesAGivenClientIDAndAppliesTheSettings(t *testing.T) {
	f := newFakeMCP(t)
	f.handle("POST", "/mcp", unauthorized(""))
	f.json("GET", "/.well-known/oauth-protected-resource/mcp", map[string]any{"authorization_servers": []string{f.URL}, "scopes_supported": []string{"read"}})
	as := asMetadata(f.URL)
	as["authorization_response_iss_parameter_supported"] = true
	f.json("GET", "/.well-known/oauth-authorization-server", as)

	cfg, err := localService(Config{}).AutoConfigure(context.Background(), f.URL+"/mcp", "my-client", "my-secret")
	require.NoError(t, err)
	assert.Equal(t, Registration{Method: models.MCPOAuthRegistrationManual, ClientID: "my-client", ClientSecret: "my-secret"}, cfg.Registration)

	var server models.MCPServer
	cfg.Apply(&server)
	assert.Equal(t, models.MCPServer{
		AuthMode:                models.MCPServerAuthModeOAuth,
		OAuthAuthURL:            f.URL + "/authorize",
		OAuthTokenURL:           f.URL + "/token",
		OAuthClientID:           "my-client",
		OAuthScopes:             []string{"read"},
		OAuthPKCEPolicy:         models.MCPServerPKCERequired,
		OAuthResource:           f.URL + "/mcp",
		OAuthIssuer:             f.URL,
		OAuthIssParamSupported:  true,
		OAuthClientRegistration: models.MCPOAuthRegistrationManual,
	}, server)
}

func discoveredStore(tokenURL string) *fakeStore {
	return &fakeStore{server: &models.MCPServer{
		ID:                     uuid.New(),
		UserID:                 uuid.New(),
		AuthMode:               models.MCPServerAuthModeOAuth,
		OAuthAuthURL:           "https://auth.example.com/authorize",
		OAuthTokenURL:          tokenURL,
		OAuthClientID:          "client",
		OAuthPKCEPolicy:        models.MCPServerPKCERequired,
		OAuthResource:          "https://mcp.example.com/mcp",
		OAuthIssuer:            "https://auth.example.com",
		OAuthIssParamSupported: true,
	}}
}

func TestADiscoveredConnectorSendsItsResourceEverywhere(t *testing.T) {
	var forms []url.Values
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		forms = append(forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
	}))
	defer tokenSrv.Close()
	store := discoveredStore(tokenSrv.URL)
	svc := New(store, nil, nil, Config{RedirectURL: "http://localhost:8080/api/mcp-servers/oauth/callback"})

	authURL, err := svc.StartAuth(context.Background(), store.server.UserID, store.server.ID, "")
	require.NoError(t, err)
	u, err := url.Parse(authURL)
	require.NoError(t, err)
	assert.Equal(t, "https://mcp.example.com/mcp", u.Query().Get("resource"))
	assert.Equal(t, "S256", u.Query().Get("code_challenge_method"))

	res, err := svc.HandleCallback(context.Background(), CallbackParams{State: "state", Code: "code", Iss: "https://auth.example.com"})
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, store.server.ID, res.ConnectorID)
	assert.Equal(t, store.server.UserID, res.UserID)

	store.server.OAuthRefreshToken = "rt"
	_, err = svc.refreshToken(context.Background(), store.server)
	require.NoError(t, err)

	require.Len(t, forms, 2)
	for _, f := range forms {
		assert.Equal(t, "https://mcp.example.com/mcp", f.Get("resource"))
	}
}

func TestTheCallbackChecksTheIssuer(t *testing.T) {
	for name, tc := range map[string]struct {
		iss       string
		supported bool
		ok        bool
	}{
		"matching":                        {"https://auth.example.com", true, true},
		"another issuer":                  {"https://attacker.example", false, false},
		"missing where always sent":       {"", true, false},
		"missing where it isn't promised": {"", false, true},
	} {
		t.Run(name, func(t *testing.T) {
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"at"}`))
			}))
			defer tokenSrv.Close()
			store := discoveredStore(tokenSrv.URL)
			store.server.OAuthIssParamSupported = tc.supported
			svc := New(store, nil, nil, Config{})
			res, err := svc.HandleCallback(context.Background(), CallbackParams{State: "state", Code: "code", Iss: tc.iss,
				Error: "", ErrorDescription: ""})
			require.NoError(t, err)
			assert.Equal(t, tc.ok, res.Success)
			assert.Equal(t, tc.ok, store.savedTokenSet != nil, "tokens are only saved for the right issuer")
		})
	}
}

func TestAnAnswerFromAnotherIssuerDoesntShowItsError(t *testing.T) {
	store := discoveredStore("https://auth.example.com/token")
	svc := New(store, nil, nil, Config{})
	res, err := svc.HandleCallback(context.Background(), CallbackParams{
		State: "state", Iss: "https://attacker.example", Error: "access_denied", ErrorDescription: "Call 555-0100 to restore access",
	})
	require.NoError(t, err)
	assert.False(t, res.Success)
	assert.NotContains(t, res.RedirectURL, "555-0100")
	assert.NotContains(t, res.Message, "555-0100")
}

func TestAManualConnectorIgnoresIss(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at"}`))
	}))
	defer tokenSrv.Close()
	store := discoveredStore(tokenSrv.URL)
	store.server.OAuthIssuer, store.server.OAuthResource = "", ""
	res, err := New(store, nil, nil, Config{}).HandleCallback(context.Background(), CallbackParams{State: "state", Code: "c", Iss: "https://whatever.example"})
	require.NoError(t, err)
	assert.True(t, res.Success)
}
