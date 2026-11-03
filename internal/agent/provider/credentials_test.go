package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

type capturingTransport struct{ last *http.Request }

func (c *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.last = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func static(key string) func(context.Context) string {
	return func(context.Context) string { return key }
}

func do(t *testing.T, c *http.Client, url string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	_, err = c.Do(req)
	require.NoError(t, err)
}

// Every OpenAI-compatible provider sends a different key in the same header,
// which is why a rule is scoped to a host rather than to a wire format.
func TestCredentialHTTPClient_ChoosesCredentialByHost(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://api.openai.com/v1", Style: BearerAuthorization, Resolve: static("sk-openai")},
		{BaseURL: "https://api.mistral.ai/v1", Style: BearerAuthorization, Resolve: static("sk-mistral")},
		{BaseURL: "https://api.anthropic.com", Style: AnthropicAPIKey, Resolve: static("sk-anthropic")},
	}, &http.Client{Transport: cap})

	do(t, c, "https://api.openai.com/v1/responses")
	require.Equal(t, "Bearer sk-openai", cap.last.Header.Get("Authorization"))

	do(t, c, "https://api.mistral.ai/v1/chat/completions")
	require.Equal(t, "Bearer sk-mistral", cap.last.Header.Get("Authorization"))

	do(t, c, "https://api.anthropic.com/v1/messages")
	require.Equal(t, "sk-anthropic", cap.last.Header.Get("x-api-key"))
	require.Empty(t, cap.last.Header.Get("Authorization"), "Anthropic takes its key in its own header")
}

// doWithHeaders sends a request already carrying headers, the way an SDK
// client built without a key fills them from the process environment.
func doWithHeaders(t *testing.T, c *http.Client, url string, headers map[string]string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	_, err = c.Do(req)
	require.NoError(t, err)
}

// A host with no rule must be left exactly as the caller built it.
func TestCredentialHTTPClient_LeavesUnknownHostsAlone(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://api.openai.com/v1", Style: BearerAuthorization, Resolve: static("sk-openai")},
	}, &http.Client{Transport: cap})

	doWithHeaders(t, c, "https://example.invalid/whatever", map[string]string{"Authorization": "Bearer theirs"})
	require.Equal(t, "Bearer theirs", cap.last.Header.Get("Authorization"))
}

// With no credential for this caller the request is not sent at all, even
// when the SDK filled the header from the environment: that key is the
// deployment's, and a caller without a key of its own must not spend it. The
// error names the provider, so the caller can say which key is missing.
func TestCredentialHTTPClient_NoCredentialIsNotSent(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{Provider: models.ModelProviderOpenAI, BaseURL: "https://api.openai.com/v1", Style: BearerAuthorization, Resolve: static("")},
		{Provider: models.ModelProviderAnthropic, BaseURL: "https://api.anthropic.com", Style: AnthropicAPIKey, Resolve: static("")},
	}, &http.Client{Transport: cap})

	for url, want := range map[string]models.ModelProvider{
		"https://api.openai.com/v1/responses":   models.ModelProviderOpenAI,
		"https://api.anthropic.com/v1/messages": models.ModelProviderAnthropic,
	} {
		body := &closeRecorder{Reader: strings.NewReader(`{}`)}
		req, err := http.NewRequest(http.MethodPost, url, body)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer sk-from-env")

		_, err = c.Do(req)
		var notConfigured *providerkeys.NotConfiguredError
		require.ErrorAs(t, err, &notConfigured)
		require.Equal(t, want, notConfigured.Provider)
		require.True(t, body.closed, "the request body is closed even though it was not sent")
	}
	require.Nil(t, cap.last, "nothing reached the provider")
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error { c.closed = true; return nil }

// A resolved credential replaces whatever the SDK set, in either header.
func TestCredentialHTTPClient_ResolvedCredentialReplacesTheSDKs(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://api.anthropic.com", Style: AnthropicAPIKey, Resolve: static("sk-account")},
	}, &http.Client{Transport: cap})

	doWithHeaders(t, c, "https://api.anthropic.com/v1/messages", map[string]string{
		"x-api-key":     "sk-ant-from-env",
		"Authorization": "Bearer token-from-env",
	})
	require.Equal(t, "sk-account", cap.last.Header.Get("x-api-key"))
	require.Empty(t, cap.last.Header.Get("Authorization"))
}

// An operator pointing a provider at a proxy moves both the request and the
// rule that credentials it, because the rule is built from the same base URL.
func TestCredentialHTTPClient_FollowsAConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://llm-proxy.internal.example/v1", Style: BearerAuthorization, Resolve: static("sk-proxied")},
	}, &http.Client{Transport: cap})

	do(t, c, "https://llm-proxy.internal.example/v1/chat/completions")
	require.Equal(t, "Bearer sk-proxied", cap.last.Header.Get("Authorization"))
}

// net/http forbids a RoundTripper from modifying the request it is handed.
func TestCredentialHTTPClient_DoesNotMutateTheCallersRequest(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://api.openai.com/v1", Style: BearerAuthorization, Resolve: static("sk-openai")},
	}, &http.Client{Transport: cap})

	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)
	_, err = c.Do(req)
	require.NoError(t, err)
	require.Empty(t, req.Header.Get("Authorization"), "the caller's request must come back unchanged")
}

// Under a non-vendor backend the base client is the deny-network transport.
// Dropping it would open egress the backend forbids.
func TestCredentialHTTPClient_PreservesTheBaseTransport(t *testing.T) {
	t.Parallel()
	var reached atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer srv.Close()

	denied := DenyNetworkHTTPClient()
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: srv.URL, Style: BearerAuthorization, Resolve: static("sk-openai")},
	}, denied)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	_, err = c.Do(req)
	require.ErrorIs(t, err, ErrNetworkDenied, "the refusal must come from the deny-network transport")
	require.False(t, reached.Load(), "a request reached a server the deny-network transport should have blocked")
}

// Gemini serves its OpenAI-compatible API and its native API (image generation)
// from one host, and the native API refuses a key sent as Bearer. A path-scoped
// rule takes the native paths; the host's unscoped rule keeps everything else.
func TestCredentialHTTPClient_PathScopedRuleClaimsItsPaths(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", Style: BearerAuthorization, Resolve: static("g-key")},
		{BaseURL: "https://generativelanguage.googleapis.com/v1beta/models", Style: GoogleAPIKey, PathScoped: true, Resolve: static("g-key")},
	}, &http.Client{Transport: cap})

	do(t, c, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.1-flash-image:generateContent")
	require.Equal(t, "g-key", cap.last.Header.Get("x-goog-api-key"))
	require.Empty(t, cap.last.Header.Get("Authorization"), "Google answers 401 when the key also arrives as Bearer")

	do(t, c, "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions")
	require.Equal(t, "Bearer g-key", cap.last.Header.Get("Authorization"))
	require.Empty(t, cap.last.Header.Get("x-goog-api-key"))

	do(t, c, "https://generativelanguage.googleapis.com/v1beta/files")
	require.Equal(t, "Bearer g-key", cap.last.Header.Get("Authorization"), "paths no scoped rule claims use the host's rule")

	do(t, c, "https://generativelanguage.googleapis.com/v1beta/modelsx")
	require.Equal(t, "Bearer g-key", cap.last.Header.Get("Authorization"), "a scope matches whole path segments only")
}

// The GoogleAPIKey style also strips a Bearer header the SDK set itself.
func TestCredentialHTTPClient_GoogleAPIKeyDropsAuthorization(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://generativelanguage.googleapis.com/v1beta/models", Style: GoogleAPIKey, PathScoped: true, Resolve: static("g-key")},
	}, &http.Client{Transport: cap})

	req, err := http.NewRequest(http.MethodPost, "https://generativelanguage.googleapis.com/v1beta/models/m:generateContent", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer stale")
	_, err = c.Do(req)
	require.NoError(t, err)
	require.Empty(t, cap.last.Header.Get("Authorization"))
	require.Equal(t, "g-key", cap.last.Header.Get("x-goog-api-key"))
	require.Equal(t, "Bearer stale", req.Header.Get("Authorization"), "the caller's request is not modified")
}

// A host with only a scoped rule leaves requests outside the scope alone.
func TestCredentialHTTPClient_ScopedRuleWithoutFallback(t *testing.T) {
	t.Parallel()
	cap := &capturingTransport{}
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: "https://img.example/api/images", Style: BearerAuthorization, PathScoped: true, Resolve: static("k")},
	}, &http.Client{Transport: cap})

	do(t, c, "https://img.example/api/other")
	require.Empty(t, cap.last.Header.Get("Authorization"))
	do(t, c, "https://img.example/api/images/generate")
	require.Equal(t, "Bearer k", cap.last.Header.Get("Authorization"))
}
