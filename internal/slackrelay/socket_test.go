package slackrelay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSocketMode is Slack's Socket Mode on an httptest server: every connection gets a hello,
// then the envelopes queued for it, and its acknowledgements are recorded.
type fakeSocketMode struct {
	srv   *httptest.Server
	appID string

	mu    sync.Mutex
	conns int
	acks  []string
	// script is what each connection sends after hello, by connection number (from 1).
	script map[int][]any
}

func newFakeSocketMode(t *testing.T) *fakeSocketMode {
	f := &fakeSocketMode{appID: "AVIX", script: map[int][]any{}}
	up := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		f.mu.Lock()
		f.conns++
		n := f.conns
		script := f.script[n]
		f.mu.Unlock()
		_ = ws.WriteJSON(map[string]any{"type": "hello", "num_connections": 1, "connection_info": map[string]any{"app_id": f.appID}})
		go func() {
			for {
				var ack map[string]string
				if ws.ReadJSON(&ack) != nil {
					return
				}
				f.mu.Lock()
				f.acks = append(f.acks, ack["envelope_id"])
				f.mu.Unlock()
			}
		}()
		for _, msg := range script {
			_ = ws.WriteJSON(msg)
		}
		<-r.Context().Done()
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSocketMode) OpenConnection(context.Context, string) (string, error) {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http"), nil
}

func (f *fakeSocketMode) connections() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns
}

func (f *fakeSocketMode) acked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acks...)
}

func messageEnvelope(id, text string) map[string]any {
	ev, _ := json.Marshal(Message{Type: "message", Channel: "C1", User: "UALICE", Text: text, TS: "1.1"})
	return map[string]any{"type": "events_api", "envelope_id": id, "payload": map[string]any{
		"type": "event_callback", "team_id": "T1", "api_app_id": "AVIX", "event": json.RawMessage(ev)}}
}

func TestSocketAcknowledgesDeliversAndReconnects(t *testing.T) {
	old := minBackoff
	minBackoff = 10 * time.Millisecond
	t.Cleanup(func() { minBackoff = old })

	f := newFakeSocketMode(t)
	f.script[1] = []any{messageEnvelope("env-1", "hi"), map[string]any{"type": "disconnect", "reason": "refresh_requested"}}
	f.script[2] = []any{messageEnvelope("env-2", "again")}

	var mu sync.Mutex
	var got []string
	var states []SocketState
	sock := OpenSocket(context.Background(), SocketConfig{
		AppToken: "xapp-1", Opener: f, Dialer: websocket.DefaultDialer,
		OnEvent: func(cb EventCallback) {
			var m Message
			require.NoError(t, json.Unmarshal(cb.Event, &m))
			mu.Lock()
			got = append(got, m.Text)
			mu.Unlock()
		},
		OnState: func(st SocketState, _ string) {
			mu.Lock()
			states = append(states, st)
			mu.Unlock()
		},
	})
	defer sock.Close()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	}, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"hi", "again"}, got)
	assert.Equal(t, 2, f.connections(), "a refresh opens a new connection")
	require.Eventually(t, func() bool { return len(f.acked()) == 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"env-1", "env-2"}, f.acked())
	mu.Lock()
	assert.Equal(t, SocketActive, states[0])
	mu.Unlock()
}

// refusingOpener answers apps.connections.open the way Slack does for a revoked token.
type refusingOpener struct{ calls int }

func (o *refusingOpener) OpenConnection(context.Context, string) (string, error) {
	o.calls++
	return "", &Error{Method: "apps.connections.open", Code: "invalid_auth"}
}

func TestSocketStopsOnARefusedToken(t *testing.T) {
	opener := &refusingOpener{}
	states := make(chan SocketState, 4)
	sock := OpenSocket(context.Background(), SocketConfig{
		AppToken: "xapp-bad", Opener: opener,
		OnState: func(st SocketState, _ string) { states <- st },
	})
	select {
	case st := <-states:
		assert.Equal(t, SocketInvalidToken, st)
	case <-time.After(time.Second):
		t.Fatal("no state reported")
	}
	require.NoError(t, sock.Close())
	assert.Equal(t, 1, opener.calls, "a refused token is not retried")
}

func TestSocketAppID(t *testing.T) {
	f := newFakeSocketMode(t)
	id, err := SocketAppID(context.Background(), f, websocket.DefaultDialer, "xapp-1")
	require.NoError(t, err)
	assert.Equal(t, "AVIX", id)
}
