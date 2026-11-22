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
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// The reply shape captured from cyanheads/sports-mcp-server 0.2.3: the server logs over MCP, so
// notifications/message events precede the result in the same SSE reply.
func chattyReply(id string) string {
	return "event: message\n" + `data: {"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","data":{"message":"Finding teams"}}}` + "\n\n" +
		"event: message\n" + `data: {"jsonrpc":"2.0","method":"notifications/message","params":{"level":"debug","data":{"message":"TSDB teams"}}}` + "\n\n" +
		"event: message\n" + `data: {"result":{"content":[{"type":"text","text":"Eagles"}]},"jsonrpc":"2.0","id":"` + id + `"}` + "\n\n"
}

func TestExtractSSEJSONPayloadSkipsServerInitiatedEvents(t *testing.T) {
	const result = `{"result":{"content":[{"type":"text","text":"ok"}]},"jsonrpc":"2.0","id":"1"}`
	const notification = `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{
			name: "result first, as before",
			body: "event: message\ndata: " + result + "\n\n",
			want: result,
		},
		{
			name: "notifications ahead of the result",
			body: "data: " + notification + "\n\ndata: " + notification + "\n\ndata: " + result + "\n\n",
			want: result,
		},
		{
			name: "result without a trailing blank line",
			body: "data: " + notification + "\n\ndata: " + result,
			want: result,
		},
		{
			name: "CRLF line endings",
			body: strings.ReplaceAll("data: "+notification+"\n\ndata: "+result+"\n\n", "\n", "\r\n"),
			want: result,
		},
		{
			name: "error response is a response",
			body: "data: " + notification + "\n\n" + `data: {"error":{"code":-32602,"message":"bad"},"jsonrpc":"2.0","id":"1"}` + "\n\n",
			want: `{"error":{"code":-32602,"message":"bad"},"jsonrpc":"2.0","id":"1"}`,
		},
		{
			name: "a server-to-client request is not the response either",
			body: `data: {"jsonrpc":"2.0","id":7,"method":"ping"}` + "\n\ndata: " + result + "\n\n",
			want: result,
		},
		{
			name: "unrecognised JSON is still accepted when nothing better arrives",
			body: `data: {"weird":true}` + "\n\n",
			want: `{"weird":true}`,
		},
		{
			name:    "only notifications is an error, not a silent empty success",
			body:    "data: " + notification + "\n\n",
			wantErr: "only server notifications",
		},
		{
			name:    "no data at all",
			body:    "event: message\n\n",
			wantErr: "did not contain a data payload",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractSSEJSONPayload([]byte(tc.body))
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
		})
	}
}

// Drives the whole call path against a server that logs over MCP, the way the sports server does.
func TestCallToolWithLogNotificationsAheadOfResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(raw, &req)
		switch req.Method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"tools": []map[string]any{{"name": "sports_find_team", "description": "d", "inputSchema": map[string]any{"type": "object"}}},
			}})
		case "tools/call":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, chattyReply(req.ID))
		}
	}))
	t.Cleanup(srv.Close)

	c := New(loopbackHTTPClient(), nil)
	server := &models.MCPServer{ID: uuid.New(), Name: "sports", ServerURL: srv.URL, AuthMode: models.MCPServerAuthModeHeader}
	servers := []*models.MCPServer{server}
	sessions := make(SessionState)

	res, err := c.DiscoverToolsWithSessionState(context.Background(), uuid.New(), servers, sessions)
	require.NoError(t, err)
	require.Len(t, res.Tools, 1)

	out, err := c.CallToolByFullNameWithSessionStateDetailed(context.Background(), servers, res.Tools[0].FullName, json.RawMessage(`{"query":"Eagles"}`), sessions)
	require.NoError(t, err)
	require.Equal(t, "Eagles", out.Output, "the result event must be used, not the first log notification")
}
