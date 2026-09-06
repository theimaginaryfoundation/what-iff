package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeProvider struct {
	getFn func(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error)
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
func (f *fakeProvider) UpdateMCPServer(context.Context, uuid.UUID, models.MCPServer, models.MCPServerAuthTokenUpdate, *[]uuid.UUID) (*models.MCPServer, error) {
	return nil, nil
}
func (f *fakeProvider) DeleteMCPServer(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type fakeProber struct {
	toolCount int
	err       error
	last      *models.MCPServer
	calls     int
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
	NewHandler(provider, prober, zap.NewNop()).RegisterRoutes(r)
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
		"server_url":"https://unsaved.example/mcp",
		"connector_id":"`+connectorID.String()+`"
	}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotNil(t, prober.last)
	require.Equal(t, "saved-token", prober.last.AuthToken)
	require.Equal(t, "https://unsaved.example/mcp", prober.last.ServerURL)
}

func TestTestMCPServerConnection_ExplicitNullAuthDoesNotFallback(t *testing.T) {
	t.Parallel()

	connectorID := uuid.New()
	prober := &fakeProber{toolCount: 1}
	provider := &fakeProvider{
		getFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*models.MCPServer, error) {
			t.Fatalf("GetMCPServer should not be called when authentication is explicitly null")
			return nil, nil
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
		req := newAuthedRequest(t, http.MethodPost, "/mcp-servers/test-connection", []byte(`{"server_url":"https://bad.example/mcp"}`))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var resp testMCPServerConnectionResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.False(t, resp.Pass)
		require.Zero(t, resp.ToolCount)
		require.NotEmpty(t, resp.Message)
	})
}
