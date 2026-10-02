package telemetry

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// defaultDependencyHosts maps the default vendor API hosts to dependency names. Base URL
// overrides from configuration are added per transport with WithDependencyHost.
var defaultDependencyHosts = map[string]string{
	"api.openai.com":                    DependencyOpenAI,
	"api.anthropic.com":                 DependencyAnthropic,
	"api.z.ai":                          DependencyZAI,
	"generativelanguage.googleapis.com": DependencyGemini,
	"api.deepseek.com":                  DependencyDeepSeek,
	"api.mistral.ai":                    DependencyMistral,
	"dashscope-intl.aliyuncs.com":       DependencyQwen,
	"dashscope.aliyuncs.com":            DependencyQwen,
	"api.xiaomimimo.com":                DependencyXiaomi,
	"api.parallel.ai":                   DependencyParallel,
}

// HTTPTransport is an http.RoundTripper that records HTTPClientDuration for every attempt,
// labelled with the dependency (derived from the request host, never the URL), method, status
// class and, on failure, error.type. SDK retries go through the transport again, so each retry
// is its own sample.
//
// A RoundTripper returns as soon as response headers arrive, so for streamed responses (LLM
// completions) the recorded time is time to headers, not the whole stream; full LLM call time
// is covered by the gen_ai metrics.
type HTTPTransport struct {
	base    http.RoundTripper
	metrics *Metrics
	hosts   map[string]string
}

// HTTPTransportOption configures an HTTPTransport.
type HTTPTransportOption func(*HTTPTransport)

// WithTransportMetrics records on m instead of Global().
func WithTransportMetrics(m *Metrics) HTTPTransportOption {
	return func(t *HTTPTransport) { t.metrics = m }
}

// WithDependencyHost labels requests to the host of hostOrURL (a bare host or a base URL such
// as "https://api.example.com/v1") as dependency. Empty or unparsable values are ignored, so
// optional base URL overrides can be passed straight from configuration.
func WithDependencyHost(hostOrURL, dependency string) HTTPTransportOption {
	return func(t *HTTPTransport) {
		if host := hostOf(hostOrURL); host != "" && dependency != "" {
			t.hosts[host] = dependency
		}
	}
}

// NewHTTPTransport wraps base (http.DefaultTransport when nil) with attempt metrics.
func NewHTTPTransport(base http.RoundTripper, opts ...HTTPTransportOption) *HTTPTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	t := &HTTPTransport{base: base, hosts: make(map[string]string, len(defaultDependencyHosts))}
	for h, d := range defaultDependencyHosts {
		t.hosts[h] = d
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// InstrumentHTTPClient returns a copy of c (a zero client when nil) whose transport records
// attempt metrics. Timeout, redirect policy and cookie jar are kept.
func InstrumentHTTPClient(c *http.Client, opts ...HTTPTransportOption) *http.Client {
	var out http.Client
	if c != nil {
		out = *c
	}
	out.Transport = NewHTTPTransport(out.Transport, opts...)
	return &out
}

// RoundTrip implements http.RoundTripper.
func (t *HTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start)

	attrs := make([]attribute.KeyValue, 0, 4)
	attrs = append(attrs,
		AttrDependency.String(t.dependencyFor(req)),
		AttrHTTPMethod.String(boundedMethod(req.Method)),
	)
	switch {
	case err != nil:
		attrs = append(attrs, AttrErrorType.String(ClassifyError(err)))
	case resp != nil:
		attrs = append(attrs, AttrHTTPStatusClass.String(HTTPStatusClass(resp.StatusCode)))
		if et := ClassifyHTTPStatus(resp.StatusCode); et != "" {
			attrs = append(attrs, AttrErrorType.String(et))
		}
	}
	m := t.metrics
	if m == nil {
		m = Global()
	}
	m.RecordDuration(req.Context(), HTTPClientDuration, elapsed, attrs...)
	return resp, err
}

func (t *HTTPTransport) dependencyFor(req *http.Request) string {
	if req == nil || req.URL == nil {
		return DependencyOther
	}
	host := strings.ToLower(req.URL.Hostname())
	if d, ok := t.hosts[host]; ok {
		return d
	}
	if strings.HasPrefix(host, "dashscope") && strings.HasSuffix(host, ".aliyuncs.com") {
		return DependencyQwen
	}
	return DependencyOther
}

func hostOf(hostOrURL string) string {
	s := strings.TrimSpace(hostOrURL)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// boundedMethod keeps http.request.method to the standard methods (semconv's "_OTHER" for
// anything else), so a malformed request can't add a series.
func boundedMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return m
	case "":
		return http.MethodGet
	default:
		return "_OTHER"
	}
}
