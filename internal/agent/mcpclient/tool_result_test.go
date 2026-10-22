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

func TestParseToolCallResult_Output(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		output     string
		isError    bool
		structured string
	}{
		{
			name:   "text remains unchanged",
			input:  `{"content":[{"type":"text","text":"done"}]}`,
			output: "done",
		},
		{
			name:       "structured content alongside text",
			input:      `{"content":[{"type":"text","text":"created"}],"structuredContent":{"id":"new","count":0,"enabled":false}}`,
			output:     "created\nStructured content:\n{\"id\":\"new\",\"count\":0,\"enabled\":false}",
			structured: `{"id":"new","count":0,"enabled":false}`,
		},
		{
			name:       "structured content without text",
			input:      `{"content":[],"structuredContent":{"id":"new"}}`,
			output:     "Structured content:\n{\"id\":\"new\"}",
			structured: `{"id":"new"}`,
		},
		{
			name:    "error without content",
			input:   `{"content":[],"isError":true}`,
			output:  "MCP tool reported an error",
			isError: true,
		},
		{
			name:       "error with text and structured content",
			input:      `{"content":[{"type":"text","text":"rejected"}],"isError":true,"structuredContent":{"code":"conflict"}}`,
			output:     "rejected\nStructured content:\n{\"code\":\"conflict\"}",
			isError:    true,
			structured: `{"code":"conflict"}`,
		},
		{
			name:       "explicit success with empty structured object",
			input:      `{"content":[],"isError":false,"structuredContent":{}}`,
			output:     "Structured content:\n{}",
			structured: `{}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := New(nil, nil).parseToolCallResult(json.RawMessage(tt.input))
			require.True(t, ok)
			require.Equal(t, tt.output, result.Output)
			require.Equal(t, tt.isError, result.IsError)
			if tt.structured == "" {
				require.Empty(t, result.StructuredContent)
			} else {
				require.JSONEq(t, tt.structured, string(result.StructuredContent))
			}
			if tt.isError {
				require.EqualError(t, result.Err(), tt.output)
			} else {
				require.NoError(t, result.Err())
			}
		})
	}
}

func TestCallToolResult_ErrWithoutOutput(t *testing.T) {
	require.EqualError(t, (CallToolResult{IsError: true}).Err(), "MCP tool reported an error")
}

func TestParseToolCallResult_RawFallback(t *testing.T) {
	for _, input := range []string{`{"status":"ok"}`, `{"content":[{"type":"unknown"}]}`, `not json`} {
		_, ok := New(nil, nil).parseToolCallResult(json.RawMessage(input))
		require.False(t, ok, input)
	}
}

func TestCallToolByFullName_ToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "create", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "creation rejected"}}}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()
	client := New(nil, nil)
	server := &models.MCPServer{ID: uuid.New(), Name: "example", ServerURL: srv.URL, Status: models.MCPServerStatusActive}
	servers := []*models.MCPServer{server}
	fullName := "mcp__" + connectorPrefix(server) + "__create"
	ctx := context.Background()
	args := json.RawMessage(`{}`)

	detailed, err := client.CallToolByFullNameDetailed(ctx, servers, fullName, args)
	require.NoError(t, err, "detailed calls keep tool-level failures in the result")
	require.True(t, detailed.IsError)
	require.EqualError(t, detailed.Err(), "creation rejected")
	detailed, err = client.CallToolByFullNameWithSessionStateDetailed(ctx, servers, fullName, args, make(SessionState))
	require.NoError(t, err)
	require.True(t, detailed.IsError)

	output, err := client.CallToolByFullName(ctx, servers, fullName, args)
	require.Equal(t, "creation rejected", output)
	require.EqualError(t, err, output)
	output, err = client.CallToolByFullNameWithSessionState(ctx, servers, fullName, args, make(SessionState))
	require.Equal(t, "creation rejected", output)
	require.EqualError(t, err, output)
}
