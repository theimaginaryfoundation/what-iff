package slackrelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"go.uber.org/zap"
)

// SocketState is how an app's Socket Mode connection stands.
type SocketState string

const (
	// SocketActive: connected (Slack said hello).
	SocketActive SocketState = "active"
	// SocketInvalidToken: Slack refused the app-level token; the session stops.
	SocketInvalidToken SocketState = "invalid_token"
	// SocketError: Slack could not be reached or the connection failed; it keeps retrying.
	SocketError SocketState = "error"
)

// Opener asks Slack for a Socket Mode URL (*API satisfies it).
type Opener interface {
	OpenConnection(ctx context.Context, appToken string) (string, error)
}

// SocketConfig is one app's long-lived Socket Mode connection.
type SocketConfig struct {
	AppToken string
	Opener   Opener
	// Dialer opens the WebSocket (NewDialer: the public-address guard).
	Dialer *websocket.Dialer
	// OnEvent receives every Events API payload, after it was acknowledged. It runs on the read
	// loop, so it must hand work off quickly.
	OnEvent func(EventCallback)
	// OnState is told when the connection's state changes, with a detail for the owner.
	OnState func(state SocketState, detail string)
	Logger  *zap.Logger
}

// Socket is an open Socket Mode session, reconnecting until Close.
type Socket struct {
	cfg    SocketConfig
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	state SocketState
}

// Timing of a session; variables so tests can shorten them.
var (
	helloTimeout = 30 * time.Second
	pingEvery    = 30 * time.Second
	readTimeout  = 90 * time.Second
	minBackoff   = time.Second
	maxBackoff   = 2 * time.Minute
)

// NewDialer is the WebSocket dialer the plugin uses: TLS, and public addresses only (checked
// after DNS on every connection).
func NewDialer() *websocket.Dialer {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: relaykit.PublicOnlyControl("slack socket")}
	return &websocket.Dialer{NetDialContext: d.DialContext, HandshakeTimeout: 15 * time.Second}
}

// OpenSocket starts a session; it connects in the background and keeps reconnecting until
// Close, ctx ends, or Slack refuses the token.
func OpenSocket(ctx context.Context, cfg SocketConfig) *Socket {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.Dialer == nil {
		cfg.Dialer = NewDialer()
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Socket{cfg: cfg, cancel: cancel, done: make(chan struct{})}
	go s.run(ctx)
	return s
}

// Close stops the session and waits for it to finish.
func (s *Socket) Close() error {
	s.cancel()
	<-s.done
	return nil
}

func (s *Socket) run(ctx context.Context) {
	defer close(s.done)
	backoff := minBackoff
	for ctx.Err() == nil {
		connected, err := s.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if IsAuthError(err) {
			s.setState(SocketInvalidToken, "Slack refused the app-level token ("+codeOf(err)+"). Paste a new one.")
			return
		}
		wait := backoff
		var slackErr *Error
		switch {
		case errors.As(err, &slackErr) && slackErr.RetryAfter > 0:
			wait = slackErr.RetryAfter
		case err != nil:
			s.setState(SocketError, err.Error())
		}
		if connected {
			backoff, wait = minBackoff, minBackoff // it worked for a while; start over
		} else if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
		if !relaykit.SleepCtx(ctx, wait) {
			return
		}
	}
}

func codeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return err.Error()
}

func (s *Socket) setState(st SocketState, detail string) {
	s.mu.Lock()
	changed := s.state != st
	s.state = st
	s.mu.Unlock()
	if (changed || st != SocketActive) && s.cfg.OnState != nil {
		s.cfg.OnState(st, detail)
	}
}

// envelope is one Socket Mode message.
type envelope struct {
	Type           string          `json:"type"`
	EnvelopeID     string          `json:"envelope_id"`
	Payload        json.RawMessage `json:"payload"`
	Reason         string          `json:"reason"`
	RetryAttempt   int             `json:"retry_attempt"`
	ConnectionInfo struct {
		AppID string `json:"app_id"`
	} `json:"connection_info"`
}

// dial opens a fresh URL and waits for Slack's hello, returning the connection and the hello.
func dial(ctx context.Context, opener Opener, dialer *websocket.Dialer, appToken string) (*websocket.Conn, *envelope, error) {
	wsURL, err := opener.OpenConnection(ctx, appToken)
	if err != nil {
		return nil, nil, err
	}
	ws, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("slack socket: dial: %w", err)
	}
	ws.SetReadLimit(4 << 20)
	_ = ws.SetReadDeadline(time.Now().Add(helloTimeout))
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			ws.Close()
			return nil, nil, fmt.Errorf("slack socket: no hello: %w", err)
		}
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.Type == "hello" {
			return ws, &env, nil
		}
	}
}

// SocketAppID opens a Socket Mode connection with appToken, reads the app id from Slack's
// hello, and closes it: how the server checks an app-level token belongs to the same app as a
// bot token. It returns "" when the hello names no app.
func SocketAppID(ctx context.Context, opener Opener, dialer *websocket.Dialer, appToken string) (string, error) {
	if dialer == nil {
		dialer = NewDialer()
	}
	ws, hello, err := dial(ctx, opener, dialer, appToken)
	if err != nil {
		return "", err
	}
	_ = ws.Close()
	return hello.ConnectionInfo.AppID, nil
}

// connectOnce dials and reads until the connection ends or Slack asks for a new one. It
// reports whether it got as far as Slack's hello.
func (s *Socket) connectOnce(ctx context.Context) (bool, error) {
	ws, _, err := dial(ctx, s.cfg.Opener, s.cfg.Dialer, s.cfg.AppToken)
	if err != nil {
		return false, err
	}
	defer ws.Close()
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	s.setState(SocketActive, "")

	var writeMu sync.Mutex
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
			return true, fmt.Errorf("slack socket: connection lost: %w", err)
		}
		var env envelope
		if json.Unmarshal(raw, &env) != nil {
			continue
		}
		// Acknowledge first: Slack redelivers what is not acknowledged within a few seconds,
		// and handling may take longer. Redeliveries are deduplicated on the message ts.
		if env.EnvelopeID != "" {
			ack, _ := json.Marshal(map[string]string{"envelope_id": env.EnvelopeID})
			writeMu.Lock()
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			werr := ws.WriteMessage(websocket.TextMessage, ack)
			writeMu.Unlock()
			if werr != nil {
				return true, fmt.Errorf("slack socket: acknowledge: %w", werr)
			}
		}
		switch env.Type {
		case "events_api":
			var cb EventCallback
			if json.Unmarshal(env.Payload, &cb) == nil && s.cfg.OnEvent != nil {
				s.cfg.OnEvent(cb)
			}
		case "disconnect":
			// Slack refreshes connections every few hours (refresh_requested, after a warning),
			// and says link_disabled when Socket Mode was switched off for the app.
			s.cfg.Logger.Info("slack socket: Slack asked to reconnect", zap.String("reason", env.Reason))
			if env.Reason == "link_disabled" {
				return true, errors.New("socket mode is switched off for this app in Slack")
			}
			return true, nil
		}
	}
}
