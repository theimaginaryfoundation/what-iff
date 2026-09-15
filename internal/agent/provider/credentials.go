package provider

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Default hosts for providers whose base URL is not configurable, taken from
// the SDK defaults rather than restated as literals elsewhere.
const (
	// DefaultOpenAIBaseURL and DefaultAnthropicBaseURL mirror the SDK defaults
	// for the two providers whose endpoint is not configurable.
	DefaultOpenAIBaseURL    = "https://api.openai.com/v1"
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
)

// CredentialStyle is how a provider expects its key on the wire.
type CredentialStyle int

const (
	// BearerAuthorization is the OpenAI wire format: Authorization: Bearer <key>.
	// Shared by every OpenAI-compatible provider, which is exactly why a rule
	// has to be scoped to a host — they all send a different key in the same
	// header.
	BearerAuthorization CredentialStyle = iota
	// AnthropicAPIKey is x-api-key: <key>, used by Anthropic and by anything
	// speaking its Messages API.
	AnthropicAPIKey
)

// CredentialRule attaches one provider's per-request credential.
//
// The host is derived from the same base URL used to build that provider's
// client, never pinned as a constant. A constant would keep matching after an
// operator points a provider at a proxy or a regional endpoint, and the rule
// would silently stop applying — failing open into a 401 rather than into
// someone else's credential, which is safe but quiet.
type CredentialRule struct {
	// BaseURL is the provider's configured endpoint; only its host is used.
	BaseURL string
	Style   CredentialStyle
	// Resolve returns the credential for the actor on the request context.
	Resolve OpenAIKeyResolver
}

type credentialTransport struct {
	base http.RoundTripper

	mu    sync.RWMutex
	rules map[string]CredentialRule // keyed by lowercase host
}

// hostOf extracts the host from a base URL, tolerating a bare host and an
// empty string.
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "//") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (t *credentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.URL == nil {
		return base.RoundTrip(req)
	}
	t.mu.RLock()
	rule, ok := t.rules[strings.ToLower(req.URL.Hostname())]
	t.mu.RUnlock()
	if !ok || rule.Resolve == nil {
		return base.RoundTrip(req)
	}
	key := rule.Resolve(req.Context())
	if key == "" {
		// Leave whatever the SDK set. The provider's own 401 is a better
		// failure than a malformed empty credential.
		return base.RoundTrip(req)
	}
	// RoundTrip must not modify the caller's request (net/http contract).
	req = req.Clone(req.Context())
	switch rule.Style {
	case AnthropicAPIKey:
		req.Header.Set("x-api-key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return base.RoundTrip(req)
}

// CredentialHTTPClient wraps base so each request carries the credential
// belonging to the actor on its context, chosen by the request's host.
//
// Two providers sharing a host would be a configuration error rather than
// something to merge, so the later rule wins and the earlier one is dropped —
// consistent with how a duplicate key behaves anywhere else.
func CredentialHTTPClient(rules []CredentialRule, base *http.Client) *http.Client {
	byHost := make(map[string]CredentialRule, len(rules))
	for _, r := range rules {
		if h := hostOf(r.BaseURL); h != "" {
			byHost[h] = r
		}
	}
	out := &http.Client{}
	if base != nil {
		*out = *base
	}
	var baseRT http.RoundTripper
	if base != nil {
		baseRT = base.Transport
	}
	out.Transport = &credentialTransport{base: baseRT, rules: byHost}
	return out
}

// StaticKey resolves to the same credential for every actor. Used for the
// deployment-wide fallback and in tests.
func StaticKey(key string) OpenAIKeyResolver {
	k := strings.TrimSpace(key)
	return func(context.Context) string { return k }
}
