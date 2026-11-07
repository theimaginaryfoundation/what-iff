// Command mcp-demo-server is a small, unauthenticated MCP server for trying the agent interaction
// features locally (ADR 0x042): elicitation (a form and a link) and an MCP App (a counter view with
// an app-only tool). It speaks Streamable HTTP on /mcp; tool calls answer with an SSE stream so the
// server can ask the client things mid-call.
//
//	go run ./cmd/mcp-demo-server -addr :8765
//
// Then add http://localhost:8765/mcp as a connector (the API needs
// MCP_ALLOW_LOCALHOST_CONNECTIONS=true) and attach it to a thread.
//
// It is a development tool: it keeps its state in memory and trusts every caller.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type server struct {
	counter atomic.Int64
	nextID  atomic.Int64
	mu      sync.Mutex
	// waiting are our requests to the client (elicitations) awaiting its answer, by id.
	waiting map[string]chan rpcMessage
}

func main() {
	addr := flag.String("addr", ":8765", "listen address")
	flag.Parse()
	s := &server{waiting: map[string]chan rpcMessage{}}
	http.HandleFunc("/mcp", s.handle)
	log.Printf("mcp demo server on %s/mcp", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if msg.Method == "" {
		// The client's answer to one of our requests.
		s.mu.Lock()
		ch := s.waiting[string(msg.ID)]
		s.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	log.Printf("<- %s (protocol %q)", msg.Method, r.Header.Get("MCP-Protocol-Version"))
	switch msg.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", fmt.Sprintf("demo-%d", time.Now().UnixNano()))
		var p struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		log.Printf("   client asked for %s with capabilities %v", p.ProtocolVersion, p.Capabilities)
		reply(w, msg.ID, map[string]any{
			"protocolVersion": "2025-11-25",
			"serverInfo":      map[string]any{"name": "whatiff-demo", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		reply(w, msg.ID, map[string]any{"tools": tools()})
	case "resources/list":
		reply(w, msg.ID, map[string]any{"resources": []map[string]any{{"uri": counterURI, "name": "Counter", "mimeType": appMIME}}})
	case "resources/read":
		reply(w, msg.ID, map[string]any{"contents": []map[string]any{{
			"uri": counterURI, "mimeType": appMIME, "text": counterHTML,
			"_meta": map[string]any{"ui": map[string]any{"prefersBorder": true}},
		}}})
	case "tools/call":
		s.callTool(w, msg)
	case "ping":
		reply(w, msg.ID, map[string]any{})
	default:
		replyError(w, msg.ID, -32601, "method not found")
	}
}

const (
	counterURI = "ui://demo/counter"
	appMIME    = "text/html;profile=mcp-app"
)

func tools() []map[string]any {
	obj := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	return []map[string]any{
		{"name": "greet", "description": "Greets the user. Asks them (through the client) for their name and how they feel.", "inputSchema": obj(map[string]any{})},
		{"name": "connect_account", "description": "Pretends to connect an outside account: asks the user to visit a page first (URL elicitation).", "inputSchema": obj(map[string]any{})},
		{
			"name": "show_counter", "description": "Shows an interactive counter app to the user.",
			"inputSchema": obj(map[string]any{"start": map[string]any{"type": "integer", "description": "Optional starting value"}}),
			"_meta":       map[string]any{"ui": map[string]any{"resourceUri": counterURI}},
		},
		{
			"name": "increment", "description": "Adds one to the counter (for the counter app only).",
			"inputSchema": obj(map[string]any{}),
			"_meta":       map[string]any{"ui": map[string]any{"resourceUri": counterURI, "visibility": []string{"app"}}},
		},
	}
}

func (s *server) callTool(w http.ResponseWriter, msg rpcMessage) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(msg.Params, &p)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}
	result := func(v any) { send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": v}) }
	text := func(t string) map[string]any {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": t}}}
	}
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "data": "running " + p.Name}})

	switch p.Name {
	case "greet":
		answer, ok := s.ask(send, map[string]any{
			"message": "Hi! What should I call you?",
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "title": "Your name", "minLength": 1},
					"mood": map[string]any{"type": "string", "title": "How are you feeling?", "oneOf": []map[string]any{
						{"const": "great", "title": "Great"}, {"const": "ok", "title": "Okay"}, {"const": "meh", "title": "Meh"},
					}, "default": "great"},
					"emoji": map[string]any{"type": "boolean", "title": "Add an emoji", "default": true},
				},
				"required": []string{"name"},
			},
		})
		if !ok {
			result(text("No answer came back from the client."))
			return
		}
		var r struct {
			Action  string         `json:"action"`
			Content map[string]any `json:"content"`
		}
		_ = json.Unmarshal(answer.Result, &r)
		if r.Action != "accept" {
			result(text("The user chose not to say (" + r.Action + ")."))
			return
		}
		greeting := fmt.Sprintf("Hello, %v! Glad you're feeling %v.", r.Content["name"], r.Content["mood"])
		if r.Content["emoji"] == true {
			greeting += " 👋"
		}
		result(text(greeting))
	case "connect_account":
		answer, ok := s.ask(send, map[string]any{
			"mode": "url", "elicitationId": fmt.Sprintf("connect-%d", time.Now().Unix()),
			"url": "https://example.com/", "message": "Open the demo sign-in page, then come back and say you're done.",
		})
		if !ok {
			result(text("No answer came back from the client."))
			return
		}
		var r struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(answer.Result, &r)
		result(text("Connect flow ended with: " + r.Action))
	case "show_counter":
		if start, ok := p.Arguments["start"].(float64); ok {
			s.counter.Store(int64(start))
		}
		n := s.counter.Load()
		result(map[string]any{
			"content":           []map[string]any{{"type": "text", "text": fmt.Sprintf("The counter is at %d. The user can click to increment it.", n)}},
			"structuredContent": map[string]any{"count": n},
		})
	case "increment":
		n := s.counter.Add(1)
		result(map[string]any{
			"content":           []map[string]any{{"type": "text", "text": fmt.Sprintf("Counter is now %d", n)}},
			"structuredContent": map[string]any{"count": n},
		})
	default:
		send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32602, "message": "unknown tool " + p.Name}})
	}
}

// ask sends elicitation/create on the stream and waits up to 15 minutes for the client's answer.
func (s *server) ask(send func(any), params map[string]any) (rpcMessage, bool) {
	id := fmt.Sprintf("%d", 1000+s.nextID.Add(1))
	ch := make(chan rpcMessage, 1)
	s.mu.Lock()
	s.waiting[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiting, id)
		s.mu.Unlock()
	}()
	send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "elicitation/create", "params": params})
	select {
	case answer := <-ch:
		log.Printf("   elicitation %s answered: %s", id, strings.TrimSpace(string(answer.Result)))
		return answer, true
	case <-time.After(15 * time.Minute):
		return rpcMessage{}, false
	}
}

func reply(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func replyError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

// counterHTML is the counter view: a hand-written MCP App (no SDK) speaking the postMessage
// protocol, so the host's side of it is easy to follow.
const counterHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body {
    background: transparent;
    color: var(--color-text-primary, #111827);
    font: var(--font-text-md-size, 14px) / 1.45 var(--font-sans, system-ui, sans-serif);
    margin: 0;
    padding: 12px 14px;
  }
  .label { color: var(--color-text-tertiary, #6b7280); font-size: var(--font-text-xs-size, 12px); }
  .n { font-size: var(--font-heading-2xl-size, 30px); font-weight: var(--font-weight-semibold, 600); font-variant-numeric: tabular-nums; line-height: 1.25; margin: 0 0 10px; }
  .row { display: flex; flex-wrap: wrap; gap: 6px; }
  button {
    background: transparent;
    border: var(--border-width-regular, 1px) solid var(--color-border-primary, #d1d5db);
    border-radius: var(--border-radius-md, 8px);
    color: var(--color-text-primary, #111827);
    cursor: pointer;
    font: inherit;
    font-size: var(--font-text-xs-size, 12px);
    font-weight: var(--font-weight-medium, 500);
    padding: 4px 10px;
  }
  button:hover { background: var(--color-background-tertiary, #f3f4f6); }
  button:focus-visible { outline: 2px solid var(--color-ring-primary, #8c87ff); outline-offset: 1px; }
  button.primary { background: var(--color-background-inverse, #111827); border-color: transparent; color: var(--color-text-inverse, #fff); }
  button.primary:hover { opacity: .88; }
  #log { color: var(--color-text-tertiary, #6b7280); font-size: var(--font-text-xs-size, 12px); margin-top: 8px; min-height: 1em; }
</style></head><body>
  <div class="label">Demo counter</div>
  <div class="n" id="n">…</div>
  <div class="row"><button class="primary" id="inc">Add one</button><button id="tell">Tell the agent</button><button id="link">Open example.com</button></div>
  <div id="log"></div>
<script>
  let nextId = 1; const pending = {};
  const send = (m) => parent.postMessage(Object.assign({ jsonrpc: '2.0' }, m), '*');
  const request = (method, params) => new Promise((resolve, reject) => { const id = nextId++; pending[id] = { resolve, reject }; send({ id, method, params }); });
  const notify = (method, params) => send({ method, params });
  const log = (t) => { document.getElementById('log').textContent = t; };
  const show = (r) => { const c = r && r.structuredContent && r.structuredContent.count; if (c !== undefined) document.getElementById('n').textContent = c; };
  const applyHost = (c) => {
    if (c.theme) document.documentElement.style.colorScheme = c.theme;
    const vars = c.styles && c.styles.variables;
    for (const k in vars || {}) document.documentElement.style.setProperty(k, vars[k]);
  };
  const size = () => notify('ui/notifications/size-changed', { width: document.body.scrollWidth, height: Math.ceil(document.body.getBoundingClientRect().height) });
  new ResizeObserver(size).observe(document.body);
  window.addEventListener('message', (e) => {
    const m = e.data || {};
    if (m.id !== undefined && pending[m.id]) { const p = pending[m.id]; delete pending[m.id]; m.error ? p.reject(m.error) : p.resolve(m.result); return; }
    if (m.method === 'ui/notifications/tool-input') log('input: ' + JSON.stringify(m.params.arguments));
    if (m.method === 'ui/notifications/tool-result') show(m.params);
    if (m.method === 'ui/notifications/host-context-changed') applyHost(m.params);
    if (m.method === 'ui/resource-teardown') send({ id: m.id, result: {} });
  });
  document.getElementById('inc').onclick = () => request('tools/call', { name: 'increment', arguments: {} }).then(show).catch(e => log('error: ' + e.message));
  document.getElementById('tell').onclick = () => request('ui/message', { role: 'user', content: { type: 'text', text: 'The counter is at ' + document.getElementById('n').textContent + '.' } }).then(() => log('sent to the composer'));
  document.getElementById('link').onclick = () => request('ui/open-link', { url: 'https://example.com' }).catch(e => log('link: ' + e.message));
  request('ui/initialize', { protocolVersion: '2026-01-26', appCapabilities: {}, clientInfo: { name: 'demo-counter', version: '0.1.0' } }).then((r) => {
    if (r && r.hostContext) applyHost(r.hostContext);
    notify('ui/notifications/initialized', {});
    size();
  });
</script></body></html>`
