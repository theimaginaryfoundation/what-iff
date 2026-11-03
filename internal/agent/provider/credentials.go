package provider

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// Default endpoints for the two providers whose base URL is not configurable,
// mirroring the SDK defaults rather than restating them elsewhere.
const (
	DefaultOpenAIBaseURL    = "https://api.openai.com/v1"
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
)

// CredentialStyle is how a provider expects its key on the wire.
type CredentialStyle int

const (
	// BearerAuthorization is the OpenAI wire format: Authorization: Bearer <key>.
	// Shared by every OpenAI-compatible provider, which is exactly why a rule
	// is scoped to a host — they all send a different key in the same header.
	BearerAuthorization CredentialStyle = iota
	// AnthropicAPIKey is x-api-key: <key>, used by Anthropic and by anything
	// speaking its Messages API.
	AnthropicAPIKey
	// GoogleAPIKey is x-goog-api-key: <key>, used by Gemini's native API (image
	// generation). Google rejects a request whose key also arrives as a Bearer
	// token; the transport's header reset leaves only this one.
	GoogleAPIKey
)

// CredentialRule attaches one provider's credential to requests bound for it.
//
// The host is derived from the same base URL used to build that provider's
// client, never pinned as a constant. A constant would keep matching after an
// operator points a provider at a proxy or a regional endpoint, and the rule
// would silently stop applying.
type CredentialRule struct {
	// Provider names the provider in the error returned when Resolve has no
	// credential.
	Provider models.ModelProvider
	// BaseURL is the provider's configured endpoint. Its host selects the rule;
	// its path matters only when PathScoped is set.
	BaseURL string
	// PathScoped limits the rule to requests under BaseURL's path, for a second
	// API on a provider's host that wants the credential differently (Gemini's
	// native API beside its OpenAI-compatible one). A host's unscoped rule
	// covers every request no scoped rule claims.
	PathScoped bool
	Style      CredentialStyle
	// Resolve returns the credential to use for this request, or "" when the
	// caller has none, in which case the request is not sent. It is the only
	// source of credentials for the rule's host: whatever the request already
	// carried is dropped.
	Resolve func(context.Context) string
}

// credentialTransport sets the Authorization (or x-api-key) header on outgoing
// requests, choosing which credential by the request's host.
//
// It exists because the provider SDKs take a key when the client is built, and
// the client value is then copied into several independent holders. There is
// no single field to reassign later, so the credential is applied where they
// all still meet: the HTTP transport they share.
type credentialTransport struct {
	base  http.RoundTripper
	rules map[string]hostRules // keyed by lowercase host; built once, never mutated
}

// hostRules are the rules for one host: path-scoped ones, longest path first,
// then the unscoped fallback.
type hostRules struct {
	scoped   []scopedRule
	fallback *CredentialRule
}

type scopedRule struct {
	path string // BaseURL's path, with a trailing slash
	rule CredentialRule
}

// match picks the rule for a request path: the longest scoped rule whose path
// contains it, else the host's unscoped rule.
func (h hostRules) match(path string) (CredentialRule, bool) {
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	for _, s := range h.scoped {
		if strings.HasPrefix(path, s.path) {
			return s.rule, true
		}
	}
	if h.fallback != nil {
		return *h.fallback, true
	}
	return CredentialRule{}, false
}

// pathOf extracts a base URL's path with a trailing slash ("/" when it has none).
func pathOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "//") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	p := u.EscapedPath()
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
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
	rule, ok := t.rules[strings.ToLower(req.URL.Hostname())].match(req.URL.EscapedPath())
	if !ok || rule.Resolve == nil {
		return base.RoundTrip(req)
	}
	key := rule.Resolve(req.Context())
	// RoundTrip must not modify the caller's request (net/http contract).
	req = req.Clone(req.Context())
	// The resolver is the only source of credentials. The SDKs fill these
	// headers from the process environment when a client is built without a
	// key, and that key belongs to the deployment, not to this caller.
	req.Header.Del("Authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	if key == "" {
		// Not sent: the provider's 401 would say only that the key is wrong.
		// The typed error says which provider has no key. RoundTrip owns the
		// body even when it does not send (net/http contract).
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, &providerkeys.NotConfiguredError{Provider: rule.Provider}
	}
	switch rule.Style {
	case AnthropicAPIKey:
		req.Header.Set("x-api-key", key)
	case GoogleAPIKey:
		req.Header.Set("x-goog-api-key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return base.RoundTrip(req)
}

// CredentialHTTPClient wraps base so every outgoing provider request carries
// the right credential for the provider it is bound for.
//
// On a provider's host the resolver's answer always wins: any Authorization
// or x-api-key already on the request, whether set by the SDK from the
// environment or by the caller, is dropped. An empty answer sends nothing: the
// request fails with *providerkeys.NotConfiguredError. The SDKs retry any
// transport error, so that error arrives after their retries (about a second).
// Requests to other hosts are left untouched.
//
// base may be nil. When base has its own transport it is preserved — under a
// non-vendor backend that is the deny-network transport, and dropping it would
// open egress the backend forbids.
//
// Two unscoped rules (or two scoped rules with the same path) sharing a host
// would be a configuration error rather than something to merge, so the later
// rule wins.
func CredentialHTTPClient(rules []CredentialRule, base *http.Client) *http.Client {
	byHost := make(map[string]hostRules, len(rules))
	for _, r := range rules {
		h := hostOf(r.BaseURL)
		if h == "" {
			continue
		}
		hr := byHost[h]
		if r.PathScoped {
			path := pathOf(r.BaseURL)
			scoped := make([]scopedRule, 0, len(hr.scoped)+1)
			for _, s := range hr.scoped {
				if s.path != path {
					scoped = append(scoped, s)
				}
			}
			scoped = append(scoped, scopedRule{path: path, rule: r})
			sort.SliceStable(scoped, func(i, j int) bool { return len(scoped[i].path) > len(scoped[j].path) })
			hr.scoped = scoped
		} else {
			r := r
			hr.fallback = &r
		}
		byHost[h] = hr
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
