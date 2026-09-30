package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestProbeConnection_PropagatesMCPSessionHeader(t *testing.T) {
	sessionID := "sess-123"
	seenHeaders := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		seenHeaders[method] = r.Header.Get(mcpSessionHeader)
		w.Header().Set(mcpSessionHeader, sessionID)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{"tools": []map[string]any{{"name": "one"}}},
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
	require.Equal(t, 1, count)
	require.Empty(t, seenHeaders["initialize"])
	require.Equal(t, sessionID, seenHeaders["notifications/initialized"])
	require.Equal(t, sessionID, seenHeaders["tools/list"])
}

func TestCallToolByFullName_PropagatesMCPSessionHeader(t *testing.T) {
	sessionID := "sess-fastmcp"
	seenToolCallSession := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		if method == "tools/call" {
			seenToolCallSession = r.Header.Get(mcpSessionHeader)
		}
		w.Header().Set(mcpSessionHeader, sessionID)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{{"name": "echo", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "ok"}},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "fastmcp",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	sessions := SessionState{}
	out, err := client.DiscoverToolsWithSessionState(context.Background(), uuid.New(), []*models.MCPServer{server}, sessions)
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	_, err = client.CallToolByFullNameWithSessionState(context.Background(), []*models.MCPServer{server}, out.Tools[0].FullName, json.RawMessage(`{}`), sessions)
	require.NoError(t, err)
	require.Equal(t, sessionID, seenToolCallSession)
}

func TestCallToolByFullName_DefaultFlowDoesNotReuseSessionAcrossCalls(t *testing.T) {
	sawMissingSessionOnToolCall := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			w.Header().Set(mcpSessionHeader, "sess-once")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			w.Header().Set(mcpSessionHeader, "sess-once")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{{"name": "echo", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			if r.Header.Get(mcpSessionHeader) == "" {
				sawMissingSessionOnToolCall = true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "ok"}},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "fastmcp",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	out, err := client.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	_, err = client.CallToolByFullName(context.Background(), []*models.MCPServer{server}, out.Tools[0].FullName, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, sawMissingSessionOnToolCall)
}

func TestCallToolByFullName_PreservesExplicitEmptyStringArguments(t *testing.T) {
	seenThreadTS := "__unset__"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					"tools": []map[string]any{{"name": "slack_post", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			params, _ := req["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			if v, ok := args["thread_ts"]; ok {
				if s, ok := v.(string); ok {
					seenThreadTS = s
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "ok"}},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "slack",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	out, err := client.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	_, err = client.CallToolByFullName(
		context.Background(),
		[]*models.MCPServer{server},
		out.Tools[0].FullName,
		json.RawMessage(`{"thread_ts":"","text":"hello"}`),
	)
	require.NoError(t, err)
	require.Equal(t, "", seenThreadTS)
}

func TestCallToolByFullNameDetailed_ParsesTypedContentBlocks(t *testing.T) {
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	const tinyWAV = "UklGRiQAAABXQVZFZm10IBAAAAABAAEAESsAACJWAAACABAAZGF0YQAAAAA="
	const tinyBlob = "aGVsbG8="

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{{"name": "inspect_graph", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "graph summary"},
						{"type": "image", "data": tinyPNG, "mimeType": "image/png"},
						{"type": "audio", "data": tinyWAV, "mimeType": "audio/wav"},
						{"type": "blob", "data": tinyBlob, "mimeType": "application/octet-stream"},
						{"type": "image", "data": "%%%invalid%%%"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "grafana",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	out, err := client.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)

	result, err := client.CallToolByFullNameDetailed(context.Background(), []*models.MCPServer{server}, out.Tools[0].FullName, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, "graph summary", result.Output)
	require.Len(t, result.GeneratedAttachments, 3)
	require.Equal(t, "image/png", result.GeneratedAttachments[0].FileType)
	require.Equal(t, tinyPNG, result.GeneratedAttachments[0].FileContent)
	require.Equal(t, "audio/wav", result.GeneratedAttachments[1].FileType)
	require.Equal(t, "application/octet-stream", result.GeneratedAttachments[2].FileType)
}

func TestCallToolByFullNameDetailed_FallsBackToRawResultString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{{"name": "inspect", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{"status": "ok"},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "misc",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	out, err := client.DiscoverTools(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)

	result, err := client.CallToolByFullNameDetailed(context.Background(), []*models.MCPServer{server}, out.Tools[0].FullName, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"ok"}`, result.Output)
	require.Empty(t, result.GeneratedAttachments)
}

func TestAuthHeaderForServer(t *testing.T) {
	t.Run("header mode passes through token", func(t *testing.T) {
		token, err := authHeaderForServer(&models.MCPServer{
			AuthMode:  models.MCPServerAuthModeHeader,
			AuthToken: "Bearer abc",
		}, time.Now().UTC())
		require.NoError(t, err)
		require.Equal(t, "Bearer abc", token)
	})

	t.Run("oauth mode builds bearer header", func(t *testing.T) {
		expires := time.Now().UTC().Add(5 * time.Minute)
		token, err := authHeaderForServer(&models.MCPServer{
			AuthMode:                  models.MCPServerAuthModeOAuth,
			OAuthAccessToken:          "oauth-token",
			OAuthAccessTokenExpiresAt: &expires,
		}, time.Now().UTC())
		require.NoError(t, err)
		require.Equal(t, "Bearer oauth-token", token)
	})

	t.Run("oauth mode missing token errors", func(t *testing.T) {
		_, err := authHeaderForServer(&models.MCPServer{
			AuthMode: models.MCPServerAuthModeOAuth,
		}, time.Now().UTC())
		require.Error(t, err)
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

func TestProbeConnection_RejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			// Body larger than maxRPCResponseBytes to assert hard cap behavior.
			_, _ = w.Write(bytes.Repeat([]byte("x"), maxRPCResponseBytes+1))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := New(nil, nil)
	_, err := client.ProbeConnection(context.Background(), &models.MCPServer{
		ID:        uuid.New(),
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeded size limit")
}
