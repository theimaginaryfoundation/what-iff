package buzzrelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"go.uber.org/zap"
)

// SessionState is how a persona's connection to a community stands.
type SessionState string

const (
	// SessionActive: authenticated and subscribed.
	SessionActive SessionState = "active"
	// SessionNeedsAccess: the relay refused the persona (not a member, or banned).
	SessionNeedsAccess SessionState = "needs_access"
	// SessionError: the relay could not be reached or the connection failed.
	SessionError SessionState = "error"
)

// SessionConfig is one persona's long-lived connection to one community.
type SessionConfig struct {
	Endpoint Endpoint
	Signer   Signer
	// Subs are the subscriptions to hold, by id. Each is sent with a since bound: on first
	// connect Start minus Skew, on a reconnect the newest event seen minus Skew, so a short
	// outage loses nothing (redeliveries are the caller's to dedupe, on the event id).
	Subs map[string]Filter
	// OnEvent receives every verified event of a subscription. It runs on the read loop,
	// so it must hand work off quickly.
	OnEvent func(subID string, ev nostr.Event)
	// OnState is told when the connection's state changes, with a detail for the owner.
	OnState func(state SessionState, detail string)
	// OnClosed is told when the relay closes a subscription (a channel the persona may not
	// read, say), with the relay's reason.
	OnClosed func(subID, reason string)
	Logger   *zap.Logger

	// Start is the earliest event of interest on first connect (zero: now).
	Start time.Time
	// Skew widens each since bound (60 s when zero).
	Skew time.Duration
}

// Session is an open connection, reconnecting until Close.
type Session struct {
	cfg    SessionConfig
	client *Client
	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	lastSeen int64
	state    SessionState
}

// Timing of a session; variables so tests can shorten them.
var (
	authTimeout     = 15 * time.Second
	pingEvery       = 30 * time.Second
	readTimeout     = 90 * time.Second
	minBackoff      = time.Second
	maxBackoff      = time.Minute
	noAccessBackoff = 2 * time.Minute
	// stableAfter is how long a connection must stay up for its end to reset the backoff. A
	// relay that accepts the subscriptions and then drops the connection would otherwise be
	// redialled every second.
	stableAfter = time.Minute
)

// Open starts a session; it connects in the background and keeps reconnecting until Close
// or ctx ends.
func (c *Client) Open(ctx context.Context, cfg SessionConfig) *Session {
	if cfg.Skew <= 0 {
		cfg.Skew = time.Minute
	}
	if cfg.Start.IsZero() {
		cfg.Start = time.Now()
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{cfg: cfg, client: c, cancel: cancel, done: make(chan struct{}), lastSeen: cfg.Start.Unix()}
	go s.run(ctx)
	return s
}

// Close stops the session and waits for it to finish.
func (s *Session) Close() error {
	s.cancel()
	<-s.done
	return nil
}

func (s *Session) run(ctx context.Context) {
	defer close(s.done)
	backoff := minBackoff
	for ctx.Err() == nil {
		began := time.Now()
		connected, err := s.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := backoff
		switch {
		case errors.Is(err, ErrNoAccess):
			s.setState(SessionNeedsAccess, accessDetail(err))
			wait = noAccessBackoff
		case err != nil:
			s.setState(SessionError, err.Error())
		}
		if connected && time.Since(began) >= stableAfter {
			backoff = minBackoff // it worked for a while; start over
			wait = minBackoff
		} else if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
		if !relaykit.SleepCtx(ctx, wait) {
			return
		}
	}
}

func accessDetail(err error) string {
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), ErrNoAccess.Error()+":"))
	if msg == "" || msg == ErrNoAccess.Error() {
		return "The community has not let this persona in yet."
	}
	return msg
}

func (s *Session) setState(st SessionState, detail string) {
	s.mu.Lock()
	changed := s.state != st
	s.state = st
	s.mu.Unlock()
	if (changed || st != SessionActive) && s.cfg.OnState != nil {
		s.cfg.OnState(st, detail)
	}
}

// connectOnce dials, authenticates, subscribes and reads until the connection ends. It
// reports whether it got as far as subscribing.
func (s *Session) connectOnce(ctx context.Context) (bool, error) {
	ws, _, err := s.client.Dialer.DialContext(ctx, s.cfg.Endpoint.WS, nil)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer ws.Close()
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	ws.SetReadLimit(1 << 20)

	var writeMu sync.Mutex
	send := func(v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return ws.WriteMessage(websocket.TextMessage, raw)
	}

	if err := s.authenticate(ws, send); err != nil {
		return false, err
	}

	s.mu.Lock()
	since := s.lastSeen - int64(s.cfg.Skew/time.Second)
	s.mu.Unlock()
	for id, f := range s.cfg.Subs {
		filter := Filter{}
		for k, v := range f {
			filter[k] = v
		}
		if cur, ok := filter["since"].(int64); !ok || cur < since {
			filter["since"] = since
		}
		if err := send([]any{"REQ", id, filter}); err != nil {
			return false, fmt.Errorf("%w: subscribe: %v", ErrUnreachable, err)
		}
	}
	s.setState(SessionActive, "")

	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go func() {
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-t.C:
				writeMu.Lock()
				err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
				writeMu.Unlock()
				if err != nil {
					_ = ws.Close()
					return
				}
			}
		}
	}()
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(readTimeout)) })
	ws.SetPingHandler(func(data string) error {
		_ = ws.SetReadDeadline(time.Now().Add(readTimeout))
		writeMu.Lock()
		defer writeMu.Unlock()
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(10*time.Second))
	})

	for {
		_ = ws.SetReadDeadline(time.Now().Add(readTimeout))
		_, raw, err := ws.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return true, nil
			}
			return true, fmt.Errorf("%w: connection lost: %v", ErrUnreachable, err)
		}
		s.handle(raw)
	}
}

// authenticate answers the relay's NIP-42 challenge and waits for its verdict. A relay that
// sends no challenge is used unauthenticated (it is then not a Buzz relay, which always
// challenges, but reads may still work).
func (s *Session) authenticate(ws *websocket.Conn, send func(any) error) error {
	deadline := time.Now().Add(authTimeout)
	_ = ws.SetReadDeadline(deadline)
	var authID string
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			if authID == "" {
				return fmt.Errorf("%w: no authentication challenge: %v", ErrUnreachable, err)
			}
			return fmt.Errorf("%w: no answer to authentication: %v", ErrUnreachable, err)
		}
		var msg []json.RawMessage
		if json.Unmarshal(raw, &msg) != nil || len(msg) < 2 {
			continue
		}
		var typ string
		_ = json.Unmarshal(msg[0], &typ)
		switch typ {
		case "AUTH":
			var challenge string
			if json.Unmarshal(msg[1], &challenge) != nil || challenge == "" {
				continue
			}
			ev, err := nostr.AuthEvent(s.cfg.Signer.Keys, s.cfg.Endpoint.WS, challenge, s.cfg.Signer.attestationFor(nostr.KindClientAuth))
			if err != nil {
				return err
			}
			authID = ev.ID
			if err := send([]any{"AUTH", ev}); err != nil {
				return fmt.Errorf("%w: %v", ErrUnreachable, err)
			}
		case "OK":
			if len(msg) < 3 {
				continue
			}
			var id string
			var ok bool
			var reason string
			_ = json.Unmarshal(msg[1], &id)
			_ = json.Unmarshal(msg[2], &ok)
			if len(msg) > 3 {
				_ = json.Unmarshal(msg[3], &reason)
			}
			if id != authID || authID == "" {
				continue
			}
			if !ok {
				return fmt.Errorf("%w: %s", ErrNoAccess, reason)
			}
			return nil
		case "NOTICE":
			var notice string
			_ = json.Unmarshal(msg[1], &notice)
			s.cfg.Logger.Debug("buzz relay notice", zap.String("notice", notice))
		}
	}
}

// handle processes one relay message after authentication.
func (s *Session) handle(raw []byte) {
	var msg []json.RawMessage
	if json.Unmarshal(raw, &msg) != nil || len(msg) < 2 {
		return
	}
	var typ string
	_ = json.Unmarshal(msg[0], &typ)
	switch typ {
	case "EVENT":
		if len(msg) < 3 {
			return
		}
		var subID string
		var ev nostr.Event
		if json.Unmarshal(msg[1], &subID) != nil || json.Unmarshal(msg[2], &ev) != nil {
			return
		}
		if ev.Verify() != nil {
			s.cfg.Logger.Warn("buzz relay: dropping an event whose signature does not verify", zap.String("event_id", ev.ID))
			return
		}
		s.mu.Lock()
		if ev.CreatedAt > s.lastSeen && ev.CreatedAt <= time.Now().Add(time.Minute).Unix() {
			s.lastSeen = ev.CreatedAt
		}
		s.mu.Unlock()
		if s.cfg.OnEvent != nil {
			s.cfg.OnEvent(subID, ev)
		}
	case "CLOSED":
		var subID, reason string
		_ = json.Unmarshal(msg[1], &subID)
		if len(msg) > 2 {
			_ = json.Unmarshal(msg[2], &reason)
		}
		if s.cfg.OnClosed != nil {
			s.cfg.OnClosed(subID, reason)
		}
	case "NOTICE":
		var notice string
		_ = json.Unmarshal(msg[1], &notice)
		s.cfg.Logger.Debug("buzz relay notice", zap.String("notice", notice))
	}
}
