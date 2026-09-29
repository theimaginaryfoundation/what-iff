package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// sseTickServer sends headers, then one SSE comment every tick until total elapses, then
// either ends (stall=false) or holds the connection open silently until the client goes.
func sseTickServer(t *testing.T, tick, total time.Duration, stall bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher := w.(http.Flusher)
		flusher.Flush()
		end := time.After(total)
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-end:
				if stall {
					<-r.Context().Done()
				}
				return
			case <-ticker.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func getBody(ctx context.Context, c *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(io.Discard, res.Body)
	return err
}

func requireCallTimeout(t *testing.T, err error, kind string) {
	t.Helper()
	var te *CallTimeoutError
	require.ErrorAs(t, err, &te)
	require.Equal(t, kind, te.Kind)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, context.Canceled, "a timeout must not look like the user cancelling")
	require.Equal(t, telemetry.ErrorTypeTimeout, telemetry.ClassifyError(err))
	_, retryable := retryReason(err)
	require.False(t, retryable)
}

func TestCallTimeouts_NonStreamedRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	c := WithCallTimeouts(nil, CallTimeouts{Request: 50 * time.Millisecond, Stream: time.Minute, StreamIdle: time.Minute})

	requireCallTimeout(t, getBody(t.Context(), c, srv.URL), CallTimeoutRequest)
}

// A healthy stream outlives Request: once SSE headers arrive, Stream and StreamIdle apply.
func TestCallTimeouts_HealthyStreamOutlivesRequestLimit(t *testing.T) {
	t.Parallel()
	srv := sseTickServer(t, 10*time.Millisecond, 250*time.Millisecond, false)
	c := WithCallTimeouts(nil, CallTimeouts{Request: 50 * time.Millisecond, Stream: 5 * time.Second, StreamIdle: 100 * time.Millisecond})

	require.NoError(t, getBody(t.Context(), c, srv.URL))
}

func TestCallTimeouts_StalledStream(t *testing.T) {
	t.Parallel()
	srv := sseTickServer(t, 10*time.Millisecond, 50*time.Millisecond, true)
	c := WithCallTimeouts(nil, CallTimeouts{Request: time.Minute, Stream: time.Minute, StreamIdle: 100 * time.Millisecond})

	start := time.Now()
	requireCallTimeout(t, getBody(t.Context(), c, srv.URL), CallTimeoutStreamIdle)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestCallTimeouts_StreamTotal(t *testing.T) {
	t.Parallel()
	srv := sseTickServer(t, 10*time.Millisecond, time.Minute, false)
	c := WithCallTimeouts(nil, CallTimeouts{Request: time.Minute, Stream: 150 * time.Millisecond, StreamIdle: time.Minute})

	requireCallTimeout(t, getBody(t.Context(), c, srv.URL), CallTimeoutStream)
}

func TestCallTimeouts_ZeroDisables(t *testing.T) {
	t.Parallel()
	srv := sseTickServer(t, 10*time.Millisecond, 100*time.Millisecond, false)
	c := WithCallTimeouts(nil, CallTimeouts{})

	require.NoError(t, getBody(t.Context(), c, srv.URL))
}

// The caller cancelling still surfaces as context.Canceled (the user stopped the turn).
func TestCallTimeouts_CallerCancelStaysCanceled(t *testing.T) {
	t.Parallel()
	srv := sseTickServer(t, 10*time.Millisecond, time.Minute, true)
	c := WithCallTimeouts(nil, DefaultCallTimeouts())
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	err := getBody(ctx, c, srv.URL)
	require.Error(t, err)
	var te *CallTimeoutError
	require.False(t, errors.As(err, &te))
}

// End to end through the Anthropic SDK and retryLLMCall: a stream that stalls after
// message_start fails with a timeout after one attempt, and is not reported as a cancel.
func TestClaudeStreaming_StalledStreamTimesOutWithoutRetry(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: " +
			`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"test-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	httpClient := WithCallTimeouts(nil, CallTimeouts{Request: time.Minute, Stream: time.Minute, StreamIdle: 100 * time.Millisecond})
	p := NewClaudeProviderWithBaseURL("k", srv.URL, nil, httpClient)

	_, err := p.CallWithRetryStreaming(t.Context(), claudeTestParams(), func(string) {})
	requireCallTimeout(t, err, CallTimeoutStreamIdle)
	require.Equal(t, int32(1), requests.Load())
}
