package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// elicitingServer is a Streamable HTTP server whose tool asks for the user's name with
// elicitation/create on the tools/call stream, waits for the client's POSTed answer, then
// returns a result built from it.
type elicitingServer struct {
	t        *testing.T
	answers  chan json.RawMessage
	mu       sync.Mutex
	headers  []http.Header
	initBody map[string]any
	extra    func(w http.ResponseWriter, flush func()) // events to send before the elicitation
}

func newElicitingServer(t *testing.T) (*elicitingServer, *httptest.Server) {
	s := &elicitingServer{t: t, answers: make(chan json.RawMessage, 4)}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *elicitingServer) serve(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.headers = append(s.headers, r.Header.Clone())
	s.mu.Unlock()
	var msg map[string]json.RawMessage
	_ = json.Unmarshal(raw, &msg)
	var method, id string
	_ = json.Unmarshal(msg["method"], &method)
	_ = json.Unmarshal(msg["id"], &id)
	if method == "" && (msg["result"] != nil || msg["error"] != nil) {
		// The client's answer to one of our requests: only the elicitation's (id 7) is awaited.
		if string(msg["id"]) == "7" {
			s.answers <- json.RawMessage(raw)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch method {
	case "initialize":
		_ = json.Unmarshal(msg["params"], &s.initBody)
		w.Header().Set("Mcp-Session-Id", "sess-1")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2025-06-18"}})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
			"tools": []map[string]any{{"name": "greet", "inputSchema": map[string]any{"type": "object"}}},
		}})
	case "tools/call":
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		flush := func() { flusher.Flush() }
		if s.extra != nil {
			s.extra(w, flush)
		}
		fmt.Fprint(w, "event: message\ndata: "+`{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{"message":"Your name?","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}}`+"\n\n")
		flush()
		select {
		case answer := <-s.answers:
			var parsed struct {
				ID     int `json:"id"`
				Result struct {
					Action  string         `json:"action"`
					Content map[string]any `json:"content"`
				} `json:"result"`
			}
			_ = json.Unmarshal(answer, &parsed)
			text := fmt.Sprintf("id=%d action=%s name=%v", parsed.ID, parsed.Result.Action, parsed.Result.Content["name"])
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%q,\"result\":{\"content\":[{\"type\":\"text\",\"text\":%q}]}}\n\n", id, text)
			flush()
		case <-time.After(5 * time.Second):
			s.t.Error("no answer to the elicitation")
		}
	}
}

type fakeElicitor struct {
	got    []ElicitationRequest
	tool   string
	result ElicitationResult
	err    error
	wait   time.Duration
}

func (f *fakeElicitor) Elicit(ctx context.Context, _ *models.MCPServer, toolName string, req ElicitationRequest) (ElicitationResult, error) {
	f.got = append(f.got, req)
	f.tool = toolName
	if f.wait > 0 {
		time.Sleep(f.wait)
	}
	return f.result, f.err
}

func callGreet(t *testing.T, srvURL string, ctx context.Context) (CallToolResult, error) {
	t.Helper()
	c := New(nil, nil)
	server := &models.MCPServer{ID: uuid.New(), Name: "greeter", ServerURL: srvURL, AuthMode: models.MCPServerAuthModeHeader}
	servers := []*models.MCPServer{server}
	sessions := make(SessionState)
	res, err := c.DiscoverToolsWithSessionState(context.Background(), uuid.New(), servers, sessions)
	require.NoError(t, err)
	return c.CallToolByFullNameWithSessionStateDetailed(ctx, servers, res.Tools[0].FullName, json.RawMessage(`{}`), sessions)
}

func TestCallTool_AnswersElicitationMidStream(t *testing.T) {
	s, srv := newElicitingServer(t)
	s.extra = func(w http.ResponseWriter, flush func()) {
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"p1\",\"method\":\"ping\"}\n\n")
		flush()
	}
	h := &fakeElicitor{result: ElicitationResult{Action: "accept", Content: map[string]any{"name": "Gori"}}}

	out, err := callGreet(t, srv.URL, WithElicitationHandler(context.Background(), h))
	require.NoError(t, err)
	assert.Equal(t, "id=7 action=accept name=Gori", out.Output)
	require.Len(t, h.got, 1)
	assert.Equal(t, "Your name?", h.got[0].Message)
	assert.JSONEq(t, `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`, string(h.got[0].RequestedSchema))
	assert.Equal(t, "greet", h.tool)

	// It advertised elicitation and MCP Apps, asked for the newest version, and then sent the
	// version the server chose, with the session, on every later request (including the answers).
	caps := s.initBody["capabilities"].(map[string]any)
	assert.Contains(t, caps, "elicitation")
	assert.Contains(t, caps["extensions"], MCPAppsExtension)
	assert.Equal(t, ProtocolVersion, s.initBody["protocolVersion"])
	s.mu.Lock()
	defer s.mu.Unlock()
	last := s.headers[len(s.headers)-1]
	assert.Equal(t, "2025-06-18", last.Get(protocolVersionHeader))
	assert.Equal(t, "sess-1", last.Get(mcpSessionHeader))
}

func TestCallTool_ElicitationWithoutHandlerIsCancelled(t *testing.T) {
	_, srv := newElicitingServer(t)
	out, err := callGreet(t, srv.URL, context.Background())
	require.NoError(t, err)
	assert.Equal(t, "id=7 action=cancel name=<nil>", out.Output)
}

func TestCallTool_HandlerErrorEndsTheCall(t *testing.T) {
	_, srv := newElicitingServer(t)
	h := &fakeElicitor{err: context.Canceled}
	_, err := callGreet(t, srv.URL, WithElicitationHandler(context.Background(), h))
	require.ErrorIs(t, err, context.Canceled)
}

func TestIdleTimer_PausedWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := newIdleTimer(30*time.Millisecond, cancel)
	timer.pause()
	time.Sleep(60 * time.Millisecond)
	assert.NoError(t, ctx.Err(), "a paused timer does not fire")
	timer.resume()
	time.Sleep(80 * time.Millisecond)
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.True(t, timer.fired())
}

func TestCompleteURLElicitations(t *testing.T) {
	c := New(nil, nil)
	server := &models.MCPServer{Name: "x"}
	required := &RPCError{Method: "tools/call", Code: urlElicitationRequiredCode, Data: json.RawMessage(
		`{"elicitations":[{"mode":"url","elicitationId":"e1","url":"https://auth.example.com/connect","message":"Sign in"}]}`)}
	h := &fakeElicitor{result: ElicitationResult{Action: "accept"}}
	ctx := WithElicitationHandler(context.Background(), h)

	assert.True(t, c.completeURLElicitations(ctx, server, "t", fmt.Errorf("wrapped: %w", required)))
	require.Len(t, h.got, 1)
	assert.Equal(t, "https://auth.example.com/connect", h.got[0].URL)

	h.result = ElicitationResult{Action: "decline"}
	assert.False(t, c.completeURLElicitations(ctx, server, "t", required))
	assert.False(t, c.completeURLElicitations(context.Background(), server, "t", required), "no handler")
	assert.False(t, c.completeURLElicitations(ctx, server, "t", errors.New("other")))
}
