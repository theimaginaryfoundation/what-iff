package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeProvider struct {
	getFn          func(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error)
	runtimeUpdates []runtimeUpdateCall
	runtimeErr     error
}

type runtimeUpdateCall struct {
	UserID      uuid.UUID
	MCPServerID uuid.UUID
	Status      string
	Reason      string
	ToolCount   int
	CheckedAt   *time.Time
	HealthyAt   *time.Time
}

func (f *fakeProvider) CreateMCPServer(context.Context, uuid.UUID, models.MCPServer) (*models.MCPServer, error) {
	return nil, nil
}
func (f *fakeProvider) GetMCPServer(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error) {
	if f.getFn != nil {
		return f.getFn(ctx, userID, id)
	}
	return nil, datastore.ErrMCPServerNotFound
}
func (f *fakeProvider) ListMCPServers(context.Context, uuid.UUID, int, int, models.MCPServerFilters) (*models.PaginatedResponse, error) {
	return nil, nil
}
func (f *fakeProvider) UpdateMCPServer(context.Context, uuid.UUID, models.MCPServer, models.MCPServerAuthTokenUpdate, models.MCPOAuthSecretUpdate, *[]uuid.UUID) (*models.MCPServer, error) {
	return nil, nil
}
func (f *fakeProvider) UpdateMCPServerRuntimeState(_ context.Context, userID, mcpServerID uuid.UUID, status, reason string, toolCount int, checkedAt, healthyAt *time.Time) error {
	f.runtimeUpdates = append(f.runtimeUpdates, runtimeUpdateCall{
		UserID:      userID,
		MCPServerID: mcpServerID,
		Status:      status,
		Reason:      reason,
		ToolCount:   toolCount,
		CheckedAt:   checkedAt,
		HealthyAt:   healthyAt,
	})
	return f.runtimeErr
}
func (f *fakeProvider) DeleteMCPServer(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type fakeProber struct {
	toolCount int
	err       error
	last      *models.MCPServer
	calls     int
}

type fakeOAuthService struct {
	startURL     string
	startErr     error
	callbackURL  string
	callbackMsg  string
	callbackPass bool
	callbackErr  error
	lastStartID  uuid.UUID
	lastUserID   uuid.UUID
	lastRedirect string
}

func (f *fakeOAuthService) StartAuth(_ context.Context, userID, connectorID uuid.UUID, redirectAfter string) (string, error) {
	f.lastUserID = userID
	f.lastStartID = connectorID
	f.lastRedirect = redirectAfter
	if f.startErr != nil {
		return "", f.startErr
	}
	return f.startURL, nil
}

func (f *fakeOAuthService) HandleCallback(_ context.Context, _, _, _, _ string) (string, string, bool, error) {
	if f.callbackErr != nil {
		return f.callbackURL, f.callbackMsg, f.callbackPass, f.callbackErr
	}
	return f.callbackURL, "ok", true, nil
}

func (f *fakeProber) ProbeConnection(_ context.Context, server *models.MCPServer) (int, error) {
	f.calls++
	if server != nil {
		copy := *server
		f.last = &copy
	}
	if f.err != nil {
		return 0, f.err
	}
	return f.toolCount, nil
}

func newAuthedRequest(t *testing.T, method, target string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	userID := uuid.New()
	return req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
}

func newRouter(provider Provider, prober ConnectionProber) *mux.Router {
	r := mux.NewRouter()
	NewHandler(provider, prober, nil, zap.NewNop(), Config{}).RegisterRoutes(r)
	return r
}

func newRouterWithConfig(provider Provider, prober ConnectionProber, cfg Config) *mux.Router {
	r := mux.NewRouter()
	NewHandler(provider, prober, nil, zap.NewNop(), cfg).RegisterRoutes(r)
	return r
}

func newRouterWithOAuth(provider Provider, prober ConnectionProber, oauth OAuthService) *mux.Router {
	r := mux.NewRouter()
	h := NewHandler(provider, prober, oauth, zap.NewNop(), Config{})
	h.RegisterRoutes(r)
	h.RegisterPublicRoutes(r)
	return r
}

func TestTestMCPServerConnection_Success(t *testing.T) {
	t.Parallel()

	prober := &fakeProber{toolCount: 3}
	router := newRouter(&fakeProvider{}, prober)
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{
		"server_url":"https://example.com/mcp",
		"authentication":"Bearer abc123"
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, 1, prober.calls)
	require.NotNil(t, prober.last)
	require.Equal(t, "https://example.com/mcp", prober.last.ServerURL)
	require.Equal(t, "Bearer abc123", prober.last.AuthToken)

	var resp testMCPServerConnectionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.True(t, resp.Pass)
	require.Equal(t, 3, resp.ToolCount)
}

func TestTestMCPServerConnection_UsesStoredTokenWhenAuthOmitted(t *testing.T) {
	t.Parallel()

	connectorID := uuid.New()
	prober := &fakeProber{toolCount: 2}
	provider := &fakeProvider{
		getFn: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.MCPServer, error) {
			require.Equal(t, connectorID, id)
			return &models.MCPServer{
				ID:        connectorID,
				ServerURL: "https://saved.example/mcp",
				AuthToken: "saved-token",
			}, nil
		},
	}

	router := newRouter(provider, prober)
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{
		"server_url":"https://8.8.8.8/mcp",
		"connector_id":"`+connectorID.String()+`"
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotNil(t, prober.last)
	require.Equal(t, "saved-token", prober.last.AuthToken)
	require.Equal(t, "https://8.8.8.8/mcp", prober.last.ServerURL)
}

func TestTestMCPServerConnection_ExplicitNullAuthClearsSavedToken(t *testing.T) {
	t.Parallel()

	connectorID := uuid.New()
	prober := &fakeProber{toolCount: 1}
	getCalls := 0
	provider := &fakeProvider{
		getFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*models.MCPServer, error) {
			getCalls++
			return &models.MCPServer{
				ID:        connectorID,
				ServerURL: "https://saved.example/mcp",
				AuthMode:  models.MCPServerAuthModeHeader,
				AuthToken: "saved-token",
			}, nil
		},
	}

	router := newRouter(provider, prober)
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{
		"server_url":"https://example.com/mcp",
		"connector_id":"`+connectorID.String()+`",
		"authentication":null
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotNil(t, prober.last)
	// Connector settings are loaded first, then explicit null auth clears token use.
	require.Equal(t, 1, getCalls)
	require.Equal(t, "", prober.last.AuthToken)
}

func TestTestMCPServerConnection_ValidationAndFailure(t *testing.T) {
	t.Parallel()

	t.Run("missing server_url", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouter(&fakeProvider{}, prober)
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Zero(t, prober.calls)
	})

	t.Run("probe error returns pass false", func(t *testing.T) {
		prober := &fakeProber{err: context.DeadlineExceeded}
		router := newRouter(&fakeProvider{}, prober)
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"https://8.8.8.8/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var resp testMCPServerConnectionResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.False(t, resp.Pass)
		require.Zero(t, resp.ToolCount)
		require.NotEmpty(t, resp.Message)
	})

	t.Run("rejects http for non-localhost", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouter(&fakeProvider{}, prober)
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"http://example.com/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Zero(t, prober.calls)
		require.Contains(t, rr.Body.String(), "https")
	})

	t.Run("rejects localhost by default", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouter(&fakeProvider{}, prober)
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"http://localhost:9000/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Zero(t, prober.calls)
		require.Contains(t, rr.Body.String(), "localhost")
	})

	t.Run("rejects private network ip", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouter(&fakeProvider{}, prober)
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"https://10.1.2.3/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Zero(t, prober.calls)
		require.Contains(t, rr.Body.String(), "private network")
	})

	t.Run("rejects hostnames that resolve to private ip", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouterWithConfig(&fakeProvider{}, prober, Config{
			ResolveHostIPs: func(_ context.Context, host string) ([]net.IPAddr, error) {
				require.Equal(t, "internal.example.com", host)
				return []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}, nil
			},
		})
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"https://internal.example.com/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Zero(t, prober.calls)
		require.Contains(t, rr.Body.String(), "private network")
	})

	t.Run("allows hostnames that resolve to public ip", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouterWithConfig(&fakeProvider{}, prober, Config{
			ResolveHostIPs: func(_ context.Context, host string) ([]net.IPAddr, error) {
				require.Equal(t, "public.example.com", host)
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			},
		})
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"https://public.example.com/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.Equal(t, 1, prober.calls)
	})

	t.Run("rejects unresolvable hostnames", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouterWithConfig(&fakeProvider{}, prober, Config{
			ResolveHostIPs: func(_ context.Context, _ string) ([]net.IPAddr, error) {
				return nil, context.DeadlineExceeded
			},
		})
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"https://unknown.example.com/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Zero(t, prober.calls)
		require.Contains(t, rr.Body.String(), "could not be resolved")
	})

	t.Run("allows localhost when explicitly enabled", func(t *testing.T) {
		prober := &fakeProber{toolCount: 1}
		router := newRouterWithConfig(&fakeProvider{}, prober, Config{AllowLocalhostConnections: true})
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"http://localhost:9000/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.Equal(t, 1, prober.calls)
	})
}

func TestTestMCPServerConnection_SuccessWithConnectorIDUpdatesRuntimeStatus(t *testing.T) {
	t.Parallel()

	connectorID := uuid.New()
	provider := &fakeProvider{
		getFn: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.MCPServer, error) {
			require.Equal(t, connectorID, id)
			return &models.MCPServer{
				ID:        connectorID,
				ServerURL: "https://example.com/mcp",
				AuthToken: "stored-token",
			}, nil
		},
	}
	prober := &fakeProber{toolCount: 4}
	router := newRouter(provider, prober)
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{
		"server_url":"https://example.com/mcp",
		"connector_id":"`+connectorID.String()+`",
		"authentication":"Bearer abc123"
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Len(t, provider.runtimeUpdates, 1)
	update := provider.runtimeUpdates[0]
	require.Equal(t, connectorID, update.MCPServerID)
	require.Equal(t, models.MCPServerStatusActive, update.Status)
	require.Equal(t, "", update.Reason)
	require.Equal(t, 4, update.ToolCount)
	require.NotNil(t, update.CheckedAt)
	require.NotNil(t, update.HealthyAt)
}

func TestTestMCPServerConnection_NoConnectorIDSkipsRuntimeStatusUpdate(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{}
	prober := &fakeProber{toolCount: 2}
	router := newRouter(provider, prober)
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{
		"server_url":"https://example.com/mcp",
		"authentication":"Bearer abc123"
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Empty(t, provider.runtimeUpdates)
}

func TestTestMCPServerConnection_FailureDoesNotMarkConnectorActive(t *testing.T) {
	t.Parallel()

	connectorID := uuid.New()
	provider := &fakeProvider{
		getFn: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.MCPServer, error) {
			require.Equal(t, connectorID, id)
			return &models.MCPServer{
				ID:        connectorID,
				ServerURL: "https://example.com/mcp",
				AuthToken: "stored-token",
			}, nil
		},
	}
	prober := &fakeProber{err: context.DeadlineExceeded}
	router := newRouter(provider, prober)
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{
		"server_url":"https://example.com/mcp",
		"connector_id":"`+connectorID.String()+`",
		"authentication":"Bearer abc123"
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Empty(t, provider.runtimeUpdates)
}

func TestStartMCPServerOAuth(t *testing.T) {
	t.Parallel()
	oauth := &fakeOAuthService{startURL: "https://accounts.example.com/authorize?state=abc"}
	router := newRouterWithOAuth(&fakeProvider{}, &fakeProber{}, oauth)
	connectorID := uuid.New()
	req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/"+connectorID.String()+"/oauth/start", []byte(`{"redirect_after":"http://localhost:4200/integrations"}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, connectorID, oauth.lastStartID)
	var body startMCPServerOAuthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Contains(t, body.AuthorizationURL, "authorize")
}

func TestHandleMCPServerOAuthCallbackRedirects(t *testing.T) {
	t.Parallel()
	oauth := &fakeOAuthService{callbackURL: "http://localhost:4200/integrations?oauth_status=success"}
	router := newRouterWithOAuth(&fakeProvider{}, &fakeProber{}, oauth)
	req := httptest.NewRequest(http.MethodGet, "/mcp-servers/oauth/callback?state=abc&code=xyz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusFound, rr.Code)
	require.Equal(t, oauth.callbackURL, rr.Header().Get("Location"))
}

func TestHandleMCPServerOAuthCallback_KnownErrorWithRedirectStillRedirects(t *testing.T) {
	t.Parallel()
	oauth := &fakeOAuthService{
		callbackURL:  "http://localhost:4200/integrations?oauth_status=error&oauth_message=bad_state",
		callbackErr:  datastore.ErrMCPOAuthSessionExpired,
		callbackPass: false,
	}
	router := newRouterWithOAuth(&fakeProvider{}, &fakeProber{}, oauth)
	req := httptest.NewRequest(http.MethodGet, "/mcp-servers/oauth/callback?state=abc", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusFound, rr.Code)
	require.Equal(t, oauth.callbackURL, rr.Header().Get("Location"))
}

func TestHandleMCPServerOAuthCallback_KnownErrorWithoutRedirectReturnsBadRequest(t *testing.T) {
	t.Parallel()
	oauth := &fakeOAuthService{
		callbackErr: datastore.ErrMCPOAuthSessionExpired,
	}
	router := newRouterWithOAuth(&fakeProvider{}, &fakeProber{}, oauth)
	req := httptest.NewRequest(http.MethodGet, "/mcp-servers/oauth/callback?state=abc", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}
