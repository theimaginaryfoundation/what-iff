package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Server-to-client requests (ADR 0x042). On Streamable HTTP a server may send requests of its own on
// the SSE stream that answers one of ours, ahead of the response: elicitation/create while a tool
// runs, or ping. The client must answer each with a new POST and keep reading the stream, so a
// streamed reply is read event by event rather than to the end first.

const (
	// ProtocolVersion is the MCP version the client asks for. A server that supports it answers with
	// it; an older server answers with its own, which is then sent back on every request
	// (MCP-Protocol-Version) and the client stays within what that version allows.
	ProtocolVersion = "2025-11-25"
	// protocolVersionHeader carries the negotiated version on every request after initialize.
	protocolVersionHeader = "MCP-Protocol-Version"
	// MCPAppsExtension is the MCP Apps extension id (SEP-1865).
	MCPAppsExtension = "io.modelcontextprotocol/ui"
	// MCPAppMIMEType is the content type of an MCP App's HTML resource.
	MCPAppMIMEType = "text/html;profile=mcp-app"
	// streamIdleTimeout ends a streamed call that has sent nothing (no event, no bytes) for this
	// long. It does not run while the client is waiting on the person to answer an elicitation,
	// which can take minutes; the turn's own context bounds that.
	streamIdleTimeout = 60 * time.Second
	// maxElicitationsPerCall bounds how many times one tool call may ask the person (spec: clients
	// SHOULD rate-limit); later requests are cancelled without asking.
	maxElicitationsPerCall = 5
)

// clientCapabilities is what the client tells servers it supports in initialize: elicitation in
// both modes, and the MCP Apps extension (ui:// HTML resources rendered in the chat).
func clientCapabilities() map[string]any {
	return map[string]any{
		"elicitation": map[string]any{
			"form": map[string]any{},
			"url":  map[string]any{},
		},
		"extensions": map[string]any{
			MCPAppsExtension: map[string]any{
				"mimeTypes": []string{MCPAppMIMEType},
			},
		},
	}
}

// ElicitationRequest is an elicitation/create request's params.
type ElicitationRequest struct {
	// Mode is "form" (the default when empty) or "url".
	Mode            string          `json:"mode,omitempty"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitationId,omitempty"`
}

// ElicitationResult is the client's answer: accept (with content for a form), decline or cancel.
type ElicitationResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

// ElicitationHandler asks the person on a server's behalf. It blocks until they answer; an error
// means the ask itself failed (the turn ended), and the server is told the request was cancelled.
type ElicitationHandler interface {
	Elicit(ctx context.Context, server *models.MCPServer, toolName string, req ElicitationRequest) (ElicitationResult, error)
}

type elicitationHandlerKey struct{}

// WithElicitationHandler makes calls made on ctx ask the person through h when a server elicits.
// Without one (discovery, sub-agents, turns nobody can answer) an elicitation is cancelled.
func WithElicitationHandler(ctx context.Context, h ElicitationHandler) context.Context {
	return context.WithValue(ctx, elicitationHandlerKey{}, h)
}

func elicitationHandlerFrom(ctx context.Context) ElicitationHandler {
	h, _ := ctx.Value(elicitationHandlerKey{}).(ElicitationHandler)
	return h
}

// serverMessage is one JSON-RPC message from the server: a response (result or error), a request
// (method and id) or a notification (method, no id). Its id is kept raw: servers use numbers or
// strings.
type serverMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

func (m serverMessage) isResponse() bool { return m.Result != nil || m.Error != nil }
func (m serverMessage) isRequest() bool {
	return m.Method != "" && len(m.ID) > 0 && string(m.ID) != "null"
}

// streamCall is the state of one call whose reply is an SSE stream.
type streamCall struct {
	c        *Client
	server   *models.MCPServer
	sessions SessionState
	toolName string
	handler  ElicitationHandler
	idle     *idleTimer
	asked    int
}

// readStreamedResponse reads an SSE reply event by event until the response to our request
// arrives, answering any server request on the way. Bytes read are bounded by
// maxRPCResponseBytes, server notifications are skipped (logged at debug).
func (s *streamCall) readStreamedResponse(ctx context.Context, body io.Reader) (rpcResponse, error) {
	reader := bufio.NewReader(&countingReader{r: body, max: maxRPCResponseBytes, onRead: s.idle.touch})
	var data []string
	var sawMessage bool
	var lastData string
	flush := func() (*rpcResponse, error) {
		if len(data) == 0 {
			return nil, nil
		}
		payload := strings.TrimSpace(strings.Join(data, "\n"))
		data = data[:0]
		if payload == "" || payload == "[DONE]" {
			return nil, nil
		}
		lastData = payload
		var msg serverMessage
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			return nil, nil
		}
		sawMessage = true
		switch {
		case msg.isResponse():
			out := rpcResponse{Result: msg.Result, Error: msg.Error}
			return &out, nil
		case msg.isRequest():
			if err := s.answerServerRequest(ctx, msg); err != nil {
				return nil, err
			}
		default:
			s.c.logger.Debug("mcp server notification on a reply stream", zap.String("method", msg.Method))
		}
		return nil, nil
	}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				resp, ferr := flush()
				if ferr != nil {
					return rpcResponse{}, ferr
				}
				if resp != nil {
					return *resp, nil
				}
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				resp, ferr := flush()
				if ferr != nil {
					return rpcResponse{}, ferr
				}
				if resp != nil {
					return *resp, nil
				}
				if sawMessage {
					return rpcResponse{}, fmt.Errorf("mcp sse reply carried only server notifications and no response: %s", truncateForError(lastData, 512))
				}
				return rpcResponse{}, fmt.Errorf("mcp sse response did not contain a data payload")
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				if s.idle.fired() {
					return rpcResponse{}, fmt.Errorf("mcp server sent nothing for %s", streamIdleTimeout)
				}
				return rpcResponse{}, ctxErr
			}
			return rpcResponse{}, err
		}
	}
}

// answerServerRequest answers one request the server sent on the stream, with a POST of its own.
func (s *streamCall) answerServerRequest(ctx context.Context, msg serverMessage) error {
	switch msg.Method {
	case "ping":
		return s.c.rpcRespond(ctx, s.server, msg.ID, map[string]any{}, nil, s.sessions)
	case "elicitation/create":
		var req ElicitationRequest
		if err := json.Unmarshal(msg.Params, &req); err != nil {
			return s.c.rpcRespond(ctx, s.server, msg.ID, nil, &rpcError{Code: -32602, Message: "invalid elicitation params"}, s.sessions)
		}
		result, err := s.elicit(ctx, req)
		if err != nil {
			// The turn ended while the person was asked: tell the server, then stop reading.
			_ = s.c.rpcRespond(context.WithoutCancel(ctx), s.server, msg.ID, ElicitationResult{Action: "cancel"}, nil, s.sessions)
			return err
		}
		return s.c.rpcRespond(ctx, s.server, msg.ID, result, nil, s.sessions)
	default:
		return s.c.rpcRespond(ctx, s.server, msg.ID, nil, &rpcError{Code: -32601, Message: "method not supported by this client: " + msg.Method}, s.sessions)
	}
}

// elicit asks the person through the call's handler. The idle timeout does not run meanwhile.
func (s *streamCall) elicit(ctx context.Context, req ElicitationRequest) (ElicitationResult, error) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "" && mode != "form" && mode != "url" {
		return ElicitationResult{Action: "cancel"}, nil
	}
	if s.handler == nil || s.asked >= maxElicitationsPerCall {
		return ElicitationResult{Action: "cancel"}, nil
	}
	s.asked++
	s.idle.pause()
	defer s.idle.resume()
	return s.handler.Elicit(ctx, s.server, s.toolName, req)
}

// rpcRespond POSTs our answer to a server request: a JSON-RPC response with the request's id.
func (c *Client) rpcRespond(ctx context.Context, server *models.MCPServer, id json.RawMessage, result any, rpcErr *rpcError, sessions SessionState) error {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	httpReq, err := c.newRPCRequest(reqCtx, server, payload, sessions)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("answer mcp server request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp server refused our answer to its request (status %d)", resp.StatusCode)
	}
	return nil
}

// newRPCRequest builds a POST to the server with the headers every message carries: content
// negotiation, the session, the negotiated protocol version and the connector's auth.
func (c *Client) newRPCRequest(ctx context.Context, server *models.MCPServer, payload []byte, sessions SessionState) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(server.ServerURL), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", streamableAccept)
	if sessionID := getSessionID(sessions, server); sessionID != "" {
		httpReq.Header.Set(mcpSessionHeader, sessionID)
	}
	if v := getProtocolVersion(sessions, server); v != "" {
		httpReq.Header.Set(protocolVersionHeader, v)
	}
	token, err := authHeaderForServer(server, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if token != "" {
		httpReq.Header.Set("Authorization", token)
	}
	return httpReq, nil
}

// The negotiated protocol version is kept in the turn's SessionState next to the session id.
func protocolVersionKey(server *models.MCPServer) string {
	key := sessionKeyForServer(server)
	if key == "" {
		return ""
	}
	return "protocol-version:" + key
}

func getProtocolVersion(sessions SessionState, server *models.MCPServer) string {
	if sessions == nil {
		return ""
	}
	if key := protocolVersionKey(server); key != "" {
		return sessions[key]
	}
	return ""
}

func setProtocolVersion(sessions SessionState, server *models.MCPServer, v string) {
	if sessions == nil || strings.TrimSpace(v) == "" {
		return
	}
	if key := protocolVersionKey(server); key != "" {
		sessions[key] = strings.TrimSpace(v)
	}
}

// countingReader bounds a stream's total size and reports each read (the idle timer).
type countingReader struct {
	r      io.Reader
	n      int64
	max    int64
	onRead func()
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.onRead != nil {
			c.onRead()
		}
		if c.max > 0 && c.n > c.max {
			return n, fmt.Errorf("mcp rpc response exceeded size limit (%d bytes)", c.max)
		}
	}
	return n, err
}

// idleTimer cancels a call that has gone quiet for too long. It can be paused while the client
// itself is the one waiting (on the person).
type idleTimer struct {
	mu      sync.Mutex
	timeout time.Duration
	timer   *time.Timer
	paused  bool
	expired bool
}

func newIdleTimer(timeout time.Duration, cancel context.CancelFunc) *idleTimer {
	t := &idleTimer{timeout: timeout}
	t.timer = time.AfterFunc(timeout, func() {
		t.mu.Lock()
		if t.paused {
			t.mu.Unlock()
			return
		}
		t.expired = true
		t.mu.Unlock()
		cancel()
	})
	return t
}

func (t *idleTimer) touch() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.paused && !t.expired {
		t.timer.Reset(t.timeout)
	}
}

func (t *idleTimer) pause() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.paused = true
	t.timer.Stop()
	t.mu.Unlock()
}

func (t *idleTimer) resume() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.paused = false
	if !t.expired {
		t.timer.Reset(t.timeout)
	}
	t.mu.Unlock()
}

func (t *idleTimer) stop() {
	if t == nil {
		return
	}
	t.timer.Stop()
}

func (t *idleTimer) fired() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.expired
}

// urlElicitationRequiredCode is the JSON-RPC error a server answers a request with when the person
// must first complete URL elicitations (URLElicitationRequiredError).
const urlElicitationRequiredCode = -32042

// completeURLElicitations handles a URLElicitationRequiredError on a tool call: it asks the person
// to complete each URL elicitation the error lists, and reports whether they all accepted (then the
// caller retries the call once). Any other error, or no handler on ctx, reports false.
func (c *Client) completeURLElicitations(ctx context.Context, server *models.MCPServer, toolName string, err error) bool {
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != urlElicitationRequiredCode {
		return false
	}
	handler := elicitationHandlerFrom(ctx)
	if handler == nil {
		return false
	}
	var data struct {
		Elicitations []ElicitationRequest `json:"elicitations"`
	}
	if json.Unmarshal(rpcErr.Data, &data) != nil || len(data.Elicitations) == 0 || len(data.Elicitations) > maxElicitationsPerCall {
		return false
	}
	for _, req := range data.Elicitations {
		if req.Mode != "url" || strings.TrimSpace(req.URL) == "" {
			return false
		}
		res, herr := handler.Elicit(ctx, server, toolName, req)
		if herr != nil || res.Action != "accept" {
			return false
		}
	}
	return true
}
