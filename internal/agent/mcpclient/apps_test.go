package mcpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// appServer serves a model-visible tool with a view, an app-only helper and a model-only tool.
func appServer(t *testing.T, calls *[]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(raw, &req)
		reply := func(result any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
		switch req.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": ProtocolVersion})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			reply(map[string]any{"tools": []map[string]any{
				{"name": "show_chart", "inputSchema": map[string]any{"type": "object"}, "_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://charts/view"}}},
				{"name": "refresh_data", "inputSchema": map[string]any{"type": "object"}, "_meta": map[string]any{"ui": map[string]any{"visibility": []any{"app"}}}},
				{"name": "delete_all", "inputSchema": map[string]any{"type": "object"}, "_meta": map[string]any{"ui": map[string]any{"visibility": []any{"model"}}}},
				{"name": "legacy", "inputSchema": map[string]any{"type": "object"}, "_meta": map[string]any{"ui/resourceUri": "ui://charts/legacy"}},
			}})
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			*calls = append(*calls, p.Name)
			reply(map[string]any{"content": []map[string]any{{"type": "text", "text": "ok " + p.Name}}, "structuredContent": map[string]any{"points": []int{1, 2}}})
		case "resources/read":
			reply(map[string]any{"contents": []map[string]any{{
				"uri": "ui://charts/view", "mimeType": MCPAppMIMEType, "text": "<!DOCTYPE html><html><body>chart</body></html>",
				"_meta": map[string]any{"ui": map[string]any{"csp": map[string]any{"connectDomains": []string{"https://api.example.com"}}, "prefersBorder": true}},
			}}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestApps_DiscoveryHidesAppOnlyToolsAndCarriesTheView(t *testing.T) {
	var calls []string
	srv := appServer(t, &calls)
	c := New(nil, nil)
	server := &models.MCPServer{ID: uuid.New(), Name: "charts", ServerURL: srv.URL}
	res, err := c.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.NoError(t, err)

	byName := map[string]ConnectorTool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	assert.NotContains(t, byName, "refresh_data", "an app-only tool is never offered to the model")
	assert.Equal(t, "ui://charts/view", byName["show_chart"].UIResourceURI)
	assert.Equal(t, "ui://charts/legacy", byName["legacy"].UIResourceURI, "the deprecated flat key still works")
	assert.Empty(t, byName["delete_all"].UIResourceURI)

	out, err := c.CallToolByFullNameDetailed(context.Background(), []*models.MCPServer{server}, byName["show_chart"].FullName, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "ui://charts/view", out.UIResourceURI)
	assert.Equal(t, "show_chart", out.ToolName)
	assert.Equal(t, server.ID, out.ServerID)
	assert.Contains(t, string(out.Raw), `"structuredContent"`)

	_, err = c.CallToolByFullNameDetailed(context.Background(), []*models.MCPServer{server}, "mcp__"+connectorPrefix(server)+"__refresh_data", json.RawMessage(`{}`))
	assert.ErrorContains(t, err, "only for its MCP App", "the model cannot reach an app-only tool by guessing its name")
}

func TestApps_ViewMayCallOnlyAppVisibleTools(t *testing.T) {
	var calls []string
	srv := appServer(t, &calls)
	c := New(nil, nil)
	server := &models.MCPServer{ID: uuid.New(), Name: "charts", ServerURL: srv.URL}

	raw, err := c.CallAppTool(context.Background(), server, "refresh_data", nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "ok refresh_data")
	_, err = c.CallAppTool(context.Background(), server, "show_chart", json.RawMessage(`{"x":1}`), nil)
	require.NoError(t, err, "a tool visible to both may be called by the view")
	_, err = c.CallAppTool(context.Background(), server, "delete_all", nil, nil)
	assert.ErrorIs(t, err, ErrToolNotForApps)
	_, err = c.CallAppTool(context.Background(), server, "nope", nil, nil)
	assert.Error(t, err)
	assert.Equal(t, []string{"refresh_data", "show_chart"}, calls)
}

func TestApps_ReadAppResource(t *testing.T) {
	var calls []string
	srv := appServer(t, &calls)
	c := New(nil, nil)
	server := &models.MCPServer{ID: uuid.New(), Name: "charts", ServerURL: srv.URL}

	res, err := c.ReadAppResource(context.Background(), server, "ui://charts/view", nil)
	require.NoError(t, err)
	assert.Contains(t, res.HTML, "chart")
	require.NotNil(t, res.CSP)
	assert.Equal(t, []string{"https://api.example.com"}, res.CSP.ConnectDomains)
	require.NotNil(t, res.PrefersBorder)

	_, err = c.ReadAppResource(context.Background(), server, "https://evil.example.com", nil)
	assert.Error(t, err)
}

func TestParseToolUIMeta(t *testing.T) {
	assert.Equal(t, toolUIMeta{}, parseToolUIMeta(nil))
	m := parseToolUIMeta(map[string]any{"ui": map[string]any{"resourceUri": "https://not-ui", "visibility": []any{"bogus"}}})
	assert.Empty(t, m.ResourceURI, "only ui:// resources are views")
	assert.False(t, m.visibleTo("model"))
	assert.False(t, m.visibleTo("app"), "an unknown visibility list is visible to neither")
	assert.True(t, toolUIMeta{}.visibleTo("model"))
}

func TestAppContentSecurityPolicy(t *testing.T) {
	none := AppContentSecurityPolicy(nil)
	assert.Contains(t, none, "default-src 'none'")
	assert.Contains(t, none, "connect-src 'none'")
	assert.Contains(t, none, "script-src 'unsafe-inline'")
	assert.NotContains(t, none, "'self'")

	p := AppContentSecurityPolicy(&AppCSPDomains{
		ConnectDomains:  []string{"https://api.example.com", "wss://live.example.com:8443", "https://x.com; script-src *", "'unsafe-eval'", "http://plain.example.com", "*"},
		ResourceDomains: []string{"https://*.cdn.example.com/"},
	})
	assert.Contains(t, p, "connect-src https://api.example.com wss://live.example.com:8443;")
	assert.Contains(t, p, "script-src 'unsafe-inline' https://*.cdn.example.com;")
	assert.NotContains(t, p, "unsafe-eval")
	assert.NotContains(t, p, "plain.example.com")
	assert.Equal(t, 11, strings.Count(p, ";")+1, "no directive can be smuggled in")
}

func TestWithCSPMeta(t *testing.T) {
	out := WithCSPMeta("<!DOCTYPE html>\n<html><head></head><body>x</body></html>", `default-src 'none'`)
	assert.True(t, strings.HasPrefix(out, `<!DOCTYPE html><meta http-equiv="Content-Security-Policy" content="default-src 'none'">`))
	out = WithCSPMeta("<div>fragment</div>", `a"b`)
	assert.True(t, strings.HasPrefix(out, `<meta http-equiv="Content-Security-Policy" content="ab"><div>`))
}
