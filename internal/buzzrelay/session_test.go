package buzzrelay

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
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
)

// flakyRelay accepts each connection's AUTH and subscriptions, records each REQ's since
// bound, then hangs up: a relay that drops every connection right after REQ.
type flakyRelay struct {
	mu     sync.Mutex
	dials  int
	sinces []int64
}

func (f *flakyRelay) serve(t *testing.T) *httptest.Server {
	up := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		f.mu.Lock()
		f.dials++
		f.mu.Unlock()
		_ = ws.WriteJSON([]any{"AUTH", "challenge"})
		for {
			var msg []json.RawMessage
			if ws.ReadJSON(&msg) != nil || len(msg) < 2 {
				return
			}
			var typ string
			_ = json.Unmarshal(msg[0], &typ)
			switch typ {
			case "AUTH":
				var ev nostr.Event
				_ = json.Unmarshal(msg[1], &ev)
				_ = ws.WriteJSON([]any{"OK", ev.ID, true, ""})
			case "REQ":
				var filter struct {
					Since int64 `json:"since"`
				}
				if len(msg) > 2 {
					_ = json.Unmarshal(msg[2], &filter)
				}
				f.mu.Lock()
				f.sinces = append(f.sinces, filter.Since)
				f.mu.Unlock()
				return // hang up right after the subscription
			}
		}
	}))
}

func (f *flakyRelay) counts() (int, []int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials, append([]int64(nil), f.sinces...)
}

func shortTimings(t *testing.T) {
	t.Helper()
	oldMin, oldMax, oldStable := minBackoff, maxBackoff, stableAfter
	minBackoff, maxBackoff, stableAfter = 10*time.Millisecond, 80*time.Millisecond, time.Hour
	t.Cleanup(func() { minBackoff, maxBackoff, stableAfter = oldMin, oldMax, oldStable })
}

func openFlaky(t *testing.T, f *flakyRelay, start time.Time) *Session {
	t.Helper()
	srv := f.serve(t)
	t.Cleanup(srv.Close)
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	keys, err := nostr.GenerateKeys()
	require.NoError(t, err)
	s := NewClient(true).Open(context.Background(), SessionConfig{
		Endpoint: Endpoint{WS: ws, HTTP: srv.URL},
		Signer:   Signer{Keys: keys},
		Subs:     map[string]Filter{"b:1": {"kinds": []int{KindStreamMessage}}},
		Start:    start,
	})
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// A connection that subscribes and then drops at once has not "worked for a while": the
// backoff keeps growing instead of redialling every minBackoff.
func TestAConnectionDroppedAfterSubscribingBacksOff(t *testing.T) {
	shortTimings(t)
	f := &flakyRelay{}
	openFlaky(t, f, time.Time{})
	time.Sleep(500 * time.Millisecond)
	dials, _ := f.counts()
	// 10+20+40+80+80+80+80 ms ≈ 390 ms: about 8 dials. Resetting to 10 ms each time would be ~40.
	assert.GreaterOrEqual(t, dials, 3)
	assert.Less(t, dials, 15, "redialled without backing off")
}

// The first subscription starts from Start (minus the skew), so a caller can resume from the
// newest event it saw on an earlier connection.
func TestTheFirstSubscriptionStartsFromStart(t *testing.T) {
	shortTimings(t)
	f := &flakyRelay{}
	start := time.Now().Add(-20 * time.Minute)
	openFlaky(t, f, start)
	require.Eventually(t, func() bool { _, s := f.counts(); return len(s) > 0 }, time.Second, 5*time.Millisecond)
	_, sinces := f.counts()
	assert.Equal(t, start.Add(-time.Minute).Unix(), sinces[0])
}
