package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestDiscoverToolsAndCallByFullName(t *testing.T) {
	var initializeCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			initializeCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "get-ticket",
							"description": "Get tracker ticket",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"id": map[string]any{"type": "string"},
								},
								"required": []string{"id"},
							},
						},
					},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "ok"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	connectorID := uuid.New()
	server := &models.MCPServer{
		ID:          connectorID,
		Name:        "tracker",
		ServerURL:   srv.URL,
		Status:      models.MCPServerStatusActive,
		Description: "Tracker connector",
	}
	out, err := client.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.NoError(t, err)
	require.NotEmpty(t, out.Tools)
	require.True(t, initializeCalled)
	fullName := out.Tools[0].FullName
	got, err := client.CallToolByFullName(context.Background(), []*models.MCPServer{server}, fullName, json.RawMessage(`{"id":"ABC-1"}`))
	require.NoError(t, err)
	require.Equal(t, "ok", got)
}

func TestParseFullToolNameInvalid(t *testing.T) {
	_, _, err := parseFullToolName("not-mcp")
	require.Error(t, err)
}

func TestProbeConnection(t *testing.T) {
	t.Run("success returns discovered tool count", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Contains(t, r.Header.Get("Accept"), "application/json")
			require.Contains(t, r.Header.Get("Accept"), "text/event-stream")
			defer r.Body.Close()
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			method, _ := req["method"].(string)
			switch method {
			case "initialize", "notifications/initialized":
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
			case "tools/list":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"result": map[string]any{
						"tools": []map[string]any{
							{"name": "one"},
							{"name": "two"},
						},
					},
				})
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer srv.Close()

		client := New(nil, nil)
		count, err := client.ProbeConnection(context.Background(), &models.MCPServer{
			ID:        uuid.New(),
			ServerURL: srv.URL,
			Status:    models.MCPServerStatusActive,
		})
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})

	t.Run("error is surfaced", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
		}))
		defer srv.Close()

		client := New(nil, nil)
		count, err := client.ProbeConnection(context.Background(), &models.MCPServer{
			ID:        uuid.New(),
			ServerURL: srv.URL,
		})
		require.Error(t, err)
		require.Zero(t, count)
		require.Contains(t, err.Error(), "invalid token")
	})

	t.Run("sse framed response is decoded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			method, _ := req["method"].(string)
			switch method {
			case "initialize", "notifications/initialized":
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
			case "tools/list":
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message\n"))
				_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"abc","result":{"tools":[{"name":"one"}]}}` + "\n\n"))
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer srv.Close()

		client := New(nil, nil)
		count, err := client.ProbeConnection(context.Background(), &models.MCPServer{
			ID:        uuid.New(),
			ServerURL: srv.URL,
			Status:    models.MCPServerStatusActive,
		})
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})
}

func TestDiscoverTools_ErrorWhenAllEligibleConnectorsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	client := New(nil, nil)
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "broken",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	out, err := client.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.Error(t, err)
	require.Empty(t, out.Tools)
	require.NotEmpty(t, out.Errors[server.ID])
}

func TestParseInputSchema_EmptyPropertiesRemainEmptyMap(t *testing.T) {
	props, req := parseInputSchema(map[string]any{
		"type": "object",
	})
	require.Empty(t, req)
	require.Equal(t, map[string]any{}, props)
}
