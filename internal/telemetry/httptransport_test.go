package telemetry_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: http.NoBody, Request: r}, nil
	}
}

func TestHTTPTransportStatusClassAndErrorType(t *testing.T) {
	tm := telemetrytest.New(t)
	name := telemetry.HTTPClientDuration.Name

	ok := &http.Client{Transport: telemetry.NewHTTPTransport(respond(200), telemetry.WithTransportMetrics(tm.Metrics))}
	resp, err := ok.Get("https://api.openai.com/v1/models")
	require.NoError(t, err)
	resp.Body.Close()

	limited := &http.Client{Transport: telemetry.NewHTTPTransport(respond(429), telemetry.WithTransportMetrics(tm.Metrics))}
	resp, err = limited.Post("https://api.anthropic.com/v1/messages", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, uint64(1), tm.HistogramCount(t, name,
		telemetry.AttrDependency.String(telemetry.DependencyOpenAI),
		telemetry.AttrHTTPMethod.String("GET"),
		telemetry.AttrHTTPStatusClass.String("2xx")))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name,
		telemetry.AttrDependency.String(telemetry.DependencyAnthropic),
		telemetry.AttrHTTPMethod.String("POST"),
		telemetry.AttrHTTPStatusClass.String("4xx"),
		telemetry.AttrErrorType.String(telemetry.ErrorTypeRateLimited)))
	// Success carries no error.type.
	require.ElementsMatch(t, []string{telemetry.ErrorTypeRateLimited},
		tm.AttributeValues(t, name, telemetry.AttrErrorType))
}

func TestHTTPTransportTransportError(t *testing.T) {
	tm := telemetrytest.New(t)
	failing := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("denied") })
	c := &http.Client{Transport: telemetry.NewHTTPTransport(failing, telemetry.WithTransportMetrics(tm.Metrics))}
	_, err := c.Get("https://api.mistral.ai/v1/chat")
	require.Error(t, err)

	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.HTTPClientDuration.Name,
		telemetry.AttrDependency.String(telemetry.DependencyMistral),
		telemetry.AttrErrorType.String(telemetry.ErrorTypeOther)))
	require.Empty(t, tm.AttributeValues(t, telemetry.HTTPClientDuration.Name, telemetry.AttrHTTPStatusClass))
}

func TestHTTPTransportDependencyMapping(t *testing.T) {
	cases := map[string]string{
		"https://generativelanguage.googleapis.com/v1beta/openai/chat": telemetry.DependencyGemini,
		"https://api.z.ai/api/anthropic/v1/messages":                   telemetry.DependencyZAI,
		"https://open.bigmodel.cn/api/anthropic/v1/messages":           telemetry.DependencyZAI,
		"https://dashscope-us.aliyuncs.com/compatible-mode/v1/x":       telemetry.DependencyQwen,
		"https://API.DEEPSEEK.COM/v1/chat":                             telemetry.DependencyDeepSeek,
		"https://api.xiaomimimo.com/v1/chat":                           telemetry.DependencyXiaomi,
		"https://api.parallel.ai/v1/search":                            telemetry.DependencyParallel,
		"https://user-supplied.example.com/some/path?id=123":           telemetry.DependencyOther,
	}
	for u, want := range cases {
		tm := telemetrytest.New(t)
		c := &http.Client{Transport: telemetry.NewHTTPTransport(respond(204),
			telemetry.WithTransportMetrics(tm.Metrics),
			telemetry.WithDependencyHost("https://open.bigmodel.cn/api/anthropic", telemetry.DependencyZAI),
			telemetry.WithDependencyHost("", telemetry.DependencyGemini), // ignored
		)}
		resp, err := c.Get(u)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, []string{want},
			tm.AttributeValues(t, telemetry.HTTPClientDuration.Name, telemetry.AttrDependency), u)
	}
}

func TestHTTPTransportRetriesAreSeparateAttempts(t *testing.T) {
	tm := telemetrytest.New(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := telemetry.InstrumentHTTPClient(srv.Client(),
		telemetry.WithTransportMetrics(tm.Metrics),
		telemetry.WithDependencyHost(srv.URL, telemetry.DependencyOpenAI))
	for i := 0; i < 3; i++ { // a caller-side retry loop, as SDKs do
		resp, err := c.Get(srv.URL + "/v1/chat")
		require.NoError(t, err)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
	}

	name := telemetry.HTTPClientDuration.Name
	dep := telemetry.AttrDependency.String(telemetry.DependencyOpenAI)
	require.Equal(t, uint64(3), tm.HistogramCount(t, name, dep))
	require.Equal(t, uint64(2), tm.HistogramCount(t, name, dep,
		telemetry.AttrHTTPStatusClass.String("5xx"), telemetry.AttrErrorType.String(telemetry.ErrorTypeServer)))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, dep, telemetry.AttrHTTPStatusClass.String("2xx")))
}

func TestInstrumentHTTPClientKeepsTimeout(t *testing.T) {
	base := &http.Client{Timeout: 20e9}
	c := telemetry.InstrumentHTTPClient(base)
	require.Equal(t, base.Timeout, c.Timeout)
	require.IsType(t, &telemetry.HTTPTransport{}, c.Transport)
	require.Nil(t, base.Transport, "the original client is not modified")
}
