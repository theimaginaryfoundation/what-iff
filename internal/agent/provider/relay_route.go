package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// DefaultCheaperInferenceBaseURL is the Cheaper Inference relay's OpenAI-compatible endpoint.
const DefaultCheaperInferenceBaseURL = "https://api.cheaperinference.com/v1"

// Route values reported by RelayTurn.Route and carried on metering.Usage.Route.
const (
	// RouteCheaperInference: every provider call in the turn went through the relay.
	RouteCheaperInference = "cheaperinference"
	// RouteMixed: the turn started on the relay and fell back to the model's own provider.
	RouteMixed = "mixed"
)

// retryReasonRelayFallback counts a relay call that failed and was re-sent to the
// model's own provider.
const retryReasonRelayFallback = "relay_fallback"

// CheaperInferenceModels maps catalog model names to the relay's ids. Only models
// listed here are ever routed; everything else always goes to its own provider.
// Phase 1 covers the Chat Completions vendors only (ADR 0x029). Entries were
// checked against the relay's public catalog on 2026-10-24.
var CheaperInferenceModels = map[string]string{
	"gemini-3.8-flash":       "gemini-3.8-flash",
	"gemini-3.7-flash":       "gemini-3.7-flash",
	"gemini-3.5-flash":       "gemini-3.5-flash",
	"gemini-3.1-pro-preview": "gemini-3.1-pro-preview",
	"mistral-medium-3-5":     "mistral-medium-3.5",
	"deepseek-v4-pro":        "deepseek-v4-pro",
	"qwen3.7-plus":           "qwen3.7-plus",
	"mimo-v2.6-pro":          "mimo-v2.6-pro",
}

// RelayRoute sends a Chat Completions provider's calls through a discounted relay
// for opted-in turns, and falls back to the provider's own endpoint when the relay
// fails or its circuit breaker for that model is open. One RelayRoute is shared by
// every Chat Completions provider; each provider holds it via SetRelayRoute.
//
// A call is routed only when its context carries a RelayTurn (the user opted in),
// the model is in the route table, the turn has not already fallen back, and the
// model's breaker allows it. A nil *RelayRoute routes nothing.
type RelayRoute struct {
	client  *openai.Client
	tel     *telemetry.Telemetry
	models  map[string]string
	breaker *relayBreaker
}

// NewCheaperInferenceRoute builds the relay route. When zdr is set every relay
// request carries "zdr": true, so the relay uses only providers that keep no
// prompts or responses; without it the relay's cheapest source resells capacity
// through third-party sellers' cloud accounts (ADR 0x029). The field is set on the
// client so no call path can drop it.
func NewCheaperInferenceRoute(baseURL string, zdr bool, tel *telemetry.Telemetry, httpClient *http.Client) *RelayRoute {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultCheaperInferenceBaseURL
	}
	opts := []option.RequestOption{option.WithAPIKey(""), option.WithBaseURL(baseURL), option.WithMaxRetries(0)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	if zdr {
		opts = append(opts, option.WithJSONSet("zdr", true))
	}
	client := openai.NewClient(opts...)
	return &RelayRoute{client: &client, tel: tel, models: CheaperInferenceModels, breaker: newRelayBreaker()}
}

// Routes reports whether model is in the route table.
func (r *RelayRoute) Routes(model string) bool {
	if r == nil {
		return false
	}
	_, ok := r.models[model]
	return ok
}

// target returns the relay id to send model to, or false when this call goes direct.
func (r *RelayRoute) target(ctx context.Context, model string) (string, *RelayTurn, bool) {
	turn := relayTurnFrom(ctx)
	if r == nil || turn == nil || turn.hasFallenBack() {
		return "", turn, false
	}
	relayModel, ok := r.models[model]
	if !ok || !r.breaker.allow(model) {
		return "", turn, false
	}
	return relayModel, turn, true
}

// failed records a relay failure: the breaker counts it when it says something
// about the relay's health, and the turn stays direct from here on, so a turn
// never alternates routes (its replayed history should come from one place).
func (r *RelayRoute) failed(ctx context.Context, model string, turn *RelayTurn, err error) {
	r.report(model, err)
	turn.fallBack()
	startGenAICall(ctx, r.tel, telemetry.DependencyCheaperInference, model, genAIOpChat).retry(retryReasonRelayFallback)
}

// report tells the breaker what a failed relay call says about the relay. An error
// that is about this request (a 400) still means the relay answered, so it counts as
// healthy; that also releases a half-open probe that ended in one.
func (r *RelayRoute) report(model string, err error) {
	if countsAgainstRelay(err) {
		r.breaker.failure(model)
	} else {
		r.breaker.success(model)
	}
}

// chatCompletionsNew is the package-level chatCompletionsNew with relay routing.
func (r *RelayRoute) chatCompletionsNew(ctx context.Context, tel *telemetry.Telemetry, providerName string, direct *openai.Client, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	model := string(params.Model)
	relayModel, turn, ok := r.target(ctx, model)
	if ok {
		relayParams := params
		relayParams.Model = shared.ChatModel(relayModel)
		resp, err := chatCompletionsNew(ctx, r.tel, telemetry.DependencyCheaperInference, r.client, relayParams)
		if err == nil {
			r.breaker.success(model)
			turn.served(true)
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		r.failed(ctx, model, turn, err)
	}
	resp, err := chatCompletionsNew(ctx, tel, providerName, direct, params)
	if err == nil {
		turn.served(false)
	}
	return resp, err
}

// chatCompletionsStream is the package-level chatCompletionsStream with relay
// routing. A relay stream that fails after it has delivered any output is not
// retried, because the caller has already shown that output; only a relay stream
// that fails before its first delta falls back.
func (r *RelayRoute) chatCompletionsStream(ctx context.Context, tel *telemetry.Telemetry, providerName string, direct *openai.Client, params openai.ChatCompletionNewParams, hooks chatCompletionStreamHooks) (*openai.ChatCompletion, error) {
	model := string(params.Model)
	relayModel, turn, ok := r.target(ctx, model)
	if ok {
		relayParams := params
		relayParams.Model = shared.ChatModel(relayModel)
		started := false
		resp, err := chatCompletionsStream(ctx, r.tel, telemetry.DependencyCheaperInference, r.client, relayParams, hooks.observed(func() { started = true }))
		if err == nil {
			r.breaker.success(model)
			turn.served(true)
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		if started {
			r.report(model, err)
			turn.served(true)
			return nil, err
		}
		r.failed(ctx, model, turn, err)
	}
	resp, err := chatCompletionsStream(ctx, tel, providerName, direct, params, hooks)
	if err == nil {
		turn.served(false)
	}
	return resp, err
}

// observed returns a copy of h whose callbacks also call onOutput first.
func (h chatCompletionStreamHooks) observed(onOutput func()) chatCompletionStreamHooks {
	out := h
	if h.onTextDelta != nil {
		out.onTextDelta = func(d string) { onOutput(); h.onTextDelta(d) }
	}
	if h.onReasoningDelta != nil {
		out.onReasoningDelta = func(d string) { onOutput(); h.onReasoningDelta(d) }
	}
	if h.onToolCallDelta != nil {
		out.onToolCallDelta = func(i int64, raw string) { onOutput(); h.onToolCallDelta(i, raw) }
	}
	return out
}

// countsAgainstRelay reports whether a relay error says the relay is unhealthy
// (unreachable, misconfigured, out of capacity) rather than that this request was
// bad. Every relay error still falls back; only these trip the breaker, so one
// user's oversized request cannot take the relay away from everyone.
func countsAgainstRelay(err error) bool {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) || apiErr == nil {
		return true // transport failure
	}
	switch code := apiErr.StatusCode; {
	case code >= 500:
		return true
	case code == http.StatusUnauthorized, code == http.StatusPaymentRequired, code == http.StatusForbidden,
		code == http.StatusNotFound, code == http.StatusRequestTimeout, code == http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

// RelayTurn is one opted-in turn's routing state. Every method is nil-safe: a nil
// turn is a turn that is not opted in. Its presence on the context is the
// opt-in: calls without one never touch the relay. It records which route served
// each call so the turn's usage can say how it was served.
type RelayTurn struct {
	mu           sync.Mutex
	relayCalls   int
	directCalls  int
	fellBackOnce bool
}

// NewRelayTurn starts the routing state for one opted-in turn.
func NewRelayTurn() *RelayTurn { return &RelayTurn{} }

type relayTurnKey struct{}

// WithRelayTurn attaches turn to ctx. A nil turn leaves ctx unchanged.
func WithRelayTurn(ctx context.Context, turn *RelayTurn) context.Context {
	if turn == nil {
		return ctx
	}
	return context.WithValue(ctx, relayTurnKey{}, turn)
}

func relayTurnFrom(ctx context.Context) *RelayTurn {
	turn, _ := ctx.Value(relayTurnKey{}).(*RelayTurn)
	return turn
}

func (t *RelayTurn) served(relay bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if relay {
		t.relayCalls++
	} else {
		t.directCalls++
	}
}

func (t *RelayTurn) fallBack() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fellBackOnce = true
}

func (t *RelayTurn) hasFallenBack() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fellBackOnce
}

// Route reports how the turn was served: RouteCheaperInference when every call went
// through the relay, RouteMixed when some did, and "" when none did (including a nil
// turn, i.e. the user is not opted in).
func (t *RelayTurn) Route() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t.relayCalls == 0:
		return ""
	case t.directCalls == 0:
		return RouteCheaperInference
	default:
		return RouteMixed
	}
}

// Breaker tuning: after relayBreakerThreshold consecutive unhealthy failures for a
// model, the relay is skipped for that model for relayBreakerCooldown; the next call
// after that is a single probe that closes the breaker on success or reopens it.
const (
	relayBreakerThreshold = 3
	relayBreakerCooldown  = 2 * time.Minute
)

// relayBreaker is a per-model circuit breaker. It is process-local: each server
// instance learns the relay's health from its own traffic.
type relayBreaker struct {
	mu     sync.Mutex
	now    func() time.Time
	states map[string]*relayBreakerState
}

type relayBreakerState struct {
	failures  int
	openUntil time.Time
	probing   bool
}

func newRelayBreaker() *relayBreaker {
	return &relayBreaker{now: time.Now, states: map[string]*relayBreakerState{}}
}

// allow reports whether a call for model may try the relay. While open it says no;
// once the cooldown passes it lets exactly one probe through until that probe reports.
func (b *relayBreaker) allow(model string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.states[model]
	if s == nil || s.failures < relayBreakerThreshold {
		return true
	}
	if b.now().Before(s.openUntil) || s.probing {
		return false
	}
	s.probing = true
	return true
}

func (b *relayBreaker) success(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.states, model)
}

func (b *relayBreaker) failure(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.states[model]
	if s == nil {
		s = &relayBreakerState{}
		b.states[model] = s
	}
	s.failures++
	s.probing = false
	if s.failures >= relayBreakerThreshold {
		s.openUntil = b.now().Add(relayBreakerCooldown)
	}
}
