package provider

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

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// relayTestServer answers every request with status and body, recording the
// requests it saw. A non-2xx status makes the relay (or provider) look down.
type relayTestServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	status int
	body   string
}

func newRelayTestServer(t *testing.T, status int, body string) *relayTestServer {
	t.Helper()
	s := &relayTestServer{status: status, body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		s.mu.Lock()
		s.bodies = append(s.bodies, decoded)
		status, body := s.status, s.body
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *relayTestServer) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *relayTestServer) request(i int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

func (s *relayTestServer) set(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

const relayDownJSON = `{"error":{"message":"no capacity","type":"api_error","code":"zdr_capacity_unavailable"}}`
const relayBadRequestJSON = `{"error":{"message":"bad","type":"invalid_request_error","code":"invalid_request"}}`

// routedMistral builds a Mistral provider routed through a relay at relayURL.
// mistral-medium-3-5 is in the route table and maps to mistral-medium-3.5.
func routedMistral(directURL, relayURL string) (*MistralProvider, *RelayRoute) {
	p := NewMistralProvider("k", directURL, nil, nil)
	route := NewCheaperInferenceRoute(relayURL, true, nil, nil)
	p.SetRelayRoute(route)
	return p, route
}

func mistralParams(model string) openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{Model: openai.ChatModel(model), Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}}
}

func TestRelayRoute_NotOptedInGoesDirect(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := newRelayTestServer(t, 200, chatCompletionTextJSON("r", "relay"))
	p, _ := routedMistral(direct.URL, relay.URL)

	_, err := p.Call(context.Background(), mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	require.Equal(t, 1, direct.calls())
	require.Equal(t, 0, relay.calls())
}

func TestRelayRoute_OptedInRoutedModelUsesRelay(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := newRelayTestServer(t, 200, chatCompletionTextJSON("r", "relay"))
	p, _ := routedMistral(direct.URL, relay.URL)
	turn := NewRelayTurn()
	ctx := WithRelayTurn(context.Background(), turn)

	resp, err := p.Call(ctx, mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	require.Equal(t, "relay", ExtractChatCompletionText(resp))
	require.Equal(t, 0, direct.calls())
	require.Equal(t, "mistral-medium-3.5", relay.request(0)["model"], "catalog name maps to the relay id")
	require.Equal(t, true, relay.request(0)["zdr"])
	require.Equal(t, RouteCheaperInference, turn.Route())
}

func TestRelayRoute_UnroutedModelGoesDirect(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := newRelayTestServer(t, 200, chatCompletionTextJSON("r", "relay"))
	p, _ := routedMistral(direct.URL, relay.URL)
	turn := NewRelayTurn()

	_, err := p.Call(WithRelayTurn(context.Background(), turn), mistralParams("mistral-tiny-unrouted"))
	require.NoError(t, err)
	require.Equal(t, 0, relay.calls())
	require.Equal(t, "", turn.Route())
}

func TestRelayRoute_FailureFallsBackAndTurnStaysDirect(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := newRelayTestServer(t, 200, chatCompletionTextJSON("r", "relay"))
	p, _ := routedMistral(direct.URL, relay.URL)
	turn := NewRelayTurn()
	ctx := WithRelayTurn(context.Background(), turn)

	// First call is served by the relay; the second finds it down and falls back.
	_, err := p.Call(ctx, mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	relay.set(503, relayDownJSON)
	resp, err := p.Call(ctx, mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	require.Equal(t, "direct", ExtractChatCompletionText(resp))
	require.Equal(t, "mistral-medium-3-5", direct.request(0)["model"], "the fallback sends our own id")
	require.NotContains(t, direct.request(0), "zdr", "zdr is a relay-only field")

	// The relay recovers, but this turn has already fallen back and stays direct.
	relay.set(200, chatCompletionTextJSON("r", "relay"))
	relayCalls := relay.calls()
	_, err = p.Call(ctx, mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	require.Equal(t, relayCalls, relay.calls())
	require.Equal(t, RouteMixed, turn.Route())
}

func TestRelayRoute_BreakerOpensAndProbes(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := newRelayTestServer(t, 503, relayDownJSON)
	p, route := routedMistral(direct.URL, relay.URL)
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	route.breaker.now = func() time.Time { return now }

	newTurn := func() context.Context { return WithRelayTurn(context.Background(), NewRelayTurn()) }
	for i := 0; i < relayBreakerThreshold; i++ {
		_, err := p.Call(newTurn(), mistralParams("mistral-medium-3-5"))
		require.NoError(t, err, "every failed relay call still gets an answer from the fallback")
	}
	tripped := relay.calls()

	// Open: new turns skip the relay entirely.
	_, err := p.Call(newTurn(), mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	require.Equal(t, tripped, relay.calls())

	// After the cooldown one probe goes through; it succeeds and closes the breaker.
	now = now.Add(relayBreakerCooldown + time.Second)
	relay.set(200, chatCompletionTextJSON("r", "relay"))
	turn := NewRelayTurn()
	_, err = p.Call(WithRelayTurn(context.Background(), turn), mistralParams("mistral-medium-3-5"))
	require.NoError(t, err)
	require.Equal(t, tripped+1, relay.calls())
	require.Equal(t, RouteCheaperInference, turn.Route())
	require.True(t, route.breaker.allow("mistral-medium-3-5"))
}

func TestRelayRoute_BadRequestFallsBackWithoutTrippingBreaker(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := newRelayTestServer(t, 400, relayBadRequestJSON)
	p, route := routedMistral(direct.URL, relay.URL)

	for i := 0; i < relayBreakerThreshold+1; i++ {
		_, err := p.Call(WithRelayTurn(context.Background(), NewRelayTurn()), mistralParams("mistral-medium-3-5"))
		require.NoError(t, err)
	}
	require.Equal(t, relayBreakerThreshold+1, relay.calls(), "a bad request says nothing about relay health")
	require.True(t, route.breaker.allow("mistral-medium-3-5"))
}

func TestCountsAgainstRelay(t *testing.T) {
	require.True(t, countsAgainstRelay(errors.New("connection refused")), "transport failure")
	require.False(t, countsAgainstRelay(&providerkeys.NotConfiguredError{Provider: models.ModelProviderCheaperInference}),
		"a caller without a relay key says nothing about the relay")
	require.False(t, countsAgainstRelay(fmt.Errorf("wrapped: %w", &providerkeys.NotConfiguredError{})))
}

func TestRelayBreaker_SingleProbeWhileHalfOpen(t *testing.T) {
	b := newRelayBreaker()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	for i := 0; i < relayBreakerThreshold; i++ {
		b.failure("m")
	}
	require.False(t, b.allow("m"))
	now = now.Add(relayBreakerCooldown)
	require.True(t, b.allow("m"), "first call after the cooldown probes")
	require.False(t, b.allow("m"), "concurrent calls wait for the probe")
	b.failure("m")
	require.False(t, b.allow("m"), "a failed probe reopens")
	require.True(t, b.allow("other"), "breakers are per model")
}

// sseRelayServer streams frames, then either finishes or breaks the stream with a
// malformed chunk.
func sseRelayServer(t *testing.T, status int, frames []string, broken bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(relayDownJSON))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
		if broken {
			_, _ = w.Write([]byte("data: {not json\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRelayRoute_StreamFallsBackBeforeFirstDelta(t *testing.T) {
	direct := sseRelayServer(t, 200, []string{deltaFrame("direct")}, false)
	relay := sseRelayServer(t, 503, nil, false)
	p, _ := routedMistral(direct.URL, relay.URL)
	turn := NewRelayTurn()

	var got string
	_, err := p.CallStreaming(WithRelayTurn(context.Background(), turn), mistralParams("mistral-medium-3-5"), func(d string) { got += d })
	require.NoError(t, err)
	require.Equal(t, "direct", got)
	require.Equal(t, "", turn.Route())
}

func TestRelayRoute_StreamDoesNotFallBackAfterOutput(t *testing.T) {
	direct := newRelayTestServer(t, 200, chatCompletionTextJSON("d", "direct"))
	relay := sseRelayServer(t, 200, []string{deltaFrame("partial")}, true)
	p, _ := routedMistral(direct.URL, relay.URL)

	var got string
	_, err := p.CallStreaming(WithRelayTurn(context.Background(), NewRelayTurn()), mistralParams("mistral-medium-3-5"), func(d string) { got += d })
	require.Error(t, err)
	require.Equal(t, "partial", got, "output already shown is not replayed from another provider")
	require.Equal(t, 0, direct.calls())
}

func TestRelayRoute_NilRouteAndNilTurn(t *testing.T) {
	var r *RelayRoute
	require.False(t, r.Routes("mistral-medium-3-5"))
	var turn *RelayTurn
	require.Equal(t, "", turn.Route())
	require.Equal(t, context.Background(), WithRelayTurn(context.Background(), nil))
}

// Every routed id must be a model the seed catalog serves through a Chat
// Completions provider: phase 1 routes nothing else (ADR 0x029).
func TestCheaperInferenceModels_AreChatCompletionsCatalogModels(t *testing.T) {
	for name := range CheaperInferenceModels {
		require.True(t, models.UsesOpenAIChatCompletionsAPI("", name), name)
	}
}
