package provider

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

// CallTimeouts bounds each HTTP attempt a provider SDK client makes. A zero field disables that
// limit. The limits apply per attempt, so a call retried by retryLLMCall gets a fresh budget for
// each attempt (a timed-out attempt is not retried there; see retryReason).
type CallTimeouts struct {
	// Request bounds an attempt from sending the request until the response headers arrive,
	// plus reading a non-streamed body. A non-streamed completion only sends headers once
	// generation is done, so this is the whole call for non-streamed calls.
	Request time.Duration
	// Stream bounds a streamed attempt (text/event-stream response) from sending the request
	// to the end of the stream. It replaces Request once the stream's headers arrive.
	Stream time.Duration
	// StreamIdle bounds the gap between bytes on a streamed response, so a stalled stream
	// fails without cutting off a long stream that is still delivering.
	StreamIdle time.Duration
}

// DefaultCallTimeouts are the limits used when LLM_REQUEST_TIMEOUT, LLM_STREAM_TIMEOUT and
// LLM_STREAM_IDLE_TIMEOUT are unset.
//
//   - Request, 10m: both vendor SDKs treat about ten minutes as the ceiling for a non-streamed
//     request (Anthropic's SDK refuses non-streamed requests it expects to exceed that).
//     Non-streamed reasoning calls at high effort usually finish within a few minutes.
//   - Stream, 30m: a backstop only. The largest output budgets here (ReasoningMaxOutputTokens,
//     Claude thinking) take minutes, even at slow decode rates. StreamIdle catches hangs first.
//   - StreamIdle, 5m: OpenAI Responses streams send nothing while a reasoning model thinks
//     (no reasoning summaries are requested), so the gap before the first event can
//     legitimately run for minutes. Anthropic streams carry pings, which count as activity.
func DefaultCallTimeouts() CallTimeouts {
	return CallTimeouts{
		Request:    10 * time.Minute,
		Stream:     30 * time.Minute,
		StreamIdle: 5 * time.Minute,
	}
}

// Call timeout kinds, as reported by CallTimeoutError.Kind.
const (
	CallTimeoutRequest    = "request"
	CallTimeoutStream     = "stream"
	CallTimeoutStreamIdle = "stream_idle"
)

// callTimeoutEnv names the setting behind each kind, for the error message.
var callTimeoutEnv = map[string]string{
	CallTimeoutRequest:    "LLM_REQUEST_TIMEOUT",
	CallTimeoutStream:     "LLM_STREAM_TIMEOUT",
	CallTimeoutStreamIdle: "LLM_STREAM_IDLE_TIMEOUT",
}

// CallTimeoutError is returned when an attempt exceeds one of its CallTimeouts. It matches
// context.DeadlineExceeded, so error.type is "timeout" and retryLLMCall doesn't retry it. It
// deliberately does not match context.Canceled, which the streaming paths treat as the user
// stopping the turn.
type CallTimeoutError struct {
	Kind  string
	Limit time.Duration
}

func (e *CallTimeoutError) Error() string {
	return fmt.Sprintf("llm provider %s timeout: no result within %s (%s)", e.Kind, e.Limit, callTimeoutEnv[e.Kind])
}

// Is makes errors.Is(err, context.DeadlineExceeded) true.
func (e *CallTimeoutError) Is(target error) bool { return target == context.DeadlineExceeded }

// Timeout reports true, like net.Error timeouts.
func (e *CallTimeoutError) Timeout() bool { return true }

// WithCallTimeouts returns a copy of c (nil means a default client) whose transport enforces t
// on every request. The provider SDK clients built from it get the limits whatever call they
// make. Wrap it inside telemetry.InstrumentHTTPClient so a timed-out attempt is recorded with
// error.type=timeout.
func WithCallTimeouts(c *http.Client, t CallTimeouts) *http.Client {
	var out http.Client
	if c != nil {
		out = *c
	}
	base := out.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	out.Transport = &callTimeoutTransport{base: base, timeouts: t}
	return &out
}

type callTimeoutTransport struct {
	base     http.RoundTripper
	timeouts CallTimeouts
}

func (t *callTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	w := &attemptWatch{cancel: cancel}
	start := time.Now()
	w.arm(CallTimeoutRequest, t.timeouts.Request, t.timeouts.Request)

	res, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		w.stop()
		return nil, w.override(err)
	}
	var idle time.Duration
	if isEventStream(res) {
		w.arm(CallTimeoutStream, t.timeouts.Stream, t.timeouts.Stream-time.Since(start))
		idle = t.timeouts.StreamIdle
		w.armIdle(idle)
	}
	res.Body = &watchedBody{rc: res.Body, w: w, idle: idle}
	return res, nil
}

func isEventStream(res *http.Response) bool {
	mediaType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	return mediaType == "text/event-stream"
}

// attemptWatch cancels one attempt when a limit fires and remembers which one did, so the
// transport and body can report a CallTimeoutError instead of the resulting context.Canceled.
type attemptWatch struct {
	cancel context.CancelFunc

	mu       sync.Mutex
	deadline *time.Timer
	idle     *time.Timer
	fired    *CallTimeoutError
}

// arm replaces the deadline timer: kind fires after remaining (at once when remaining <= 0),
// and reports limit. A zero limit disarms.
func (w *attemptWatch) arm(kind string, limit, remaining time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.deadline != nil {
		w.deadline.Stop()
		w.deadline = nil
	}
	if limit <= 0 {
		return
	}
	w.deadline = time.AfterFunc(max(remaining, 0), func() { w.fire(kind, limit) })
}

func (w *attemptWatch) armIdle(limit time.Duration) {
	if limit <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.idle = time.AfterFunc(limit, func() { w.fire(CallTimeoutStreamIdle, limit) })
}

// touch restarts the idle timer after the stream delivered bytes.
func (w *attemptWatch) touch(limit time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.idle != nil && w.fired == nil {
		w.idle.Reset(limit)
	}
}

func (w *attemptWatch) fire(kind string, limit time.Duration) {
	w.mu.Lock()
	if w.fired == nil {
		w.fired = &CallTimeoutError{Kind: kind, Limit: limit}
	}
	w.mu.Unlock()
	w.cancel()
}

// override returns the CallTimeoutError when a limit fired, else err.
func (w *attemptWatch) override(err error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fired != nil {
		return w.fired
	}
	return err
}

func (w *attemptWatch) stop() {
	w.mu.Lock()
	if w.deadline != nil {
		w.deadline.Stop()
	}
	if w.idle != nil {
		w.idle.Stop()
	}
	w.mu.Unlock()
	w.cancel()
}

// watchedBody reports a fired limit as the read error and releases the attempt on Close.
type watchedBody struct {
	rc   io.ReadCloser
	w    *attemptWatch
	idle time.Duration
}

func (b *watchedBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 && b.idle > 0 {
		b.w.touch(b.idle)
	}
	if err != nil && err != io.EOF {
		err = b.w.override(err)
	}
	return n, err
}

func (b *watchedBody) Close() error {
	err := b.rc.Close()
	b.w.stop()
	return err
}
