package provider

import (
	"context"
	"net/http"
	"strings"
)

// openAIAPIHost is the only host this package rewrites credentials for.
//
// Scoping by host is not optional. The shared provider HTTP client is handed to
// every OpenAI-compatible SDK client in the process — Gemini, Mistral, DeepSeek,
// Qwen, Xiaomi and z.ai all speak the OpenAI wire format and all send their own
// key in the same Authorization header. A blanket rewrite would replace each of
// their credentials with OpenAI's.
//
// If an OPENAI_BASE_URL override is ever added, this must learn about it or the
// rewrite will silently stop applying.
const openAIAPIHost = "api.openai.com"

// OpenAIKeyResolver returns the OpenAI key for the actor ctx belongs to, or ""
// when that actor has none.
//
// Keys belong to accounts, not to the process: a self-hosted instance with two
// users has two people's credentials and two people's bills. Resolving per
// request is what makes that true, and it is cheap here because the SDK client
// value is copied into six independent holders (OpenAIProvider, the recall and
// memory tools, the file-chunk pipeline, the memory handler, and the plugin
// embedder) that all share one HTTP client. Resolving at that shared transport
// reaches all six without touching a single call site.
//
// The actor survives into background work: every detach point in the agent
// goes through middleware.CopyUserToIDContext, so an agent job or a scheduled
// run still resolves to the user who owns it.
type OpenAIKeyResolver func(ctx context.Context) string

// StaticOpenAIKey resolves to the same key for every actor. Used for the
// deployment-wide fallback and in tests.
func StaticOpenAIKey(key string) OpenAIKeyResolver {
	k := strings.TrimSpace(key)
	return func(context.Context) string { return k }
}

type openAICredentialTransport struct {
	resolve OpenAIKeyResolver
	base    http.RoundTripper
}

func (t *openAICredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.URL == nil || req.URL.Hostname() != openAIAPIHost || t.resolve == nil {
		return base.RoundTrip(req)
	}
	// The SDK propagates the caller's context to the outgoing request, so this
	// is the same ctx the handler or job was running under.
	if key := t.resolve(req.Context()); key != "" {
		// RoundTrip must not modify the caller's request (net/http contract).
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return base.RoundTrip(req)
}

// OpenAICredentialHTTPClient wraps base so requests to the OpenAI API carry the
// key belonging to the actor on the request context. Pass the result wherever
// the shared provider HTTP client is expected.
//
// base may be nil, in which case http.DefaultTransport is used. When base has
// its own transport it is preserved — under a non-vendor backend that is the
// deny-network transport, and dropping it would open egress the backend
// forbids.
func OpenAICredentialHTTPClient(resolve OpenAIKeyResolver, base *http.Client) *http.Client {
	out := &http.Client{}
	if base != nil {
		*out = *base
	}
	var baseRT http.RoundTripper
	if base != nil {
		baseRT = base.Transport
	}
	out.Transport = &openAICredentialTransport{resolve: resolve, base: baseRT}
	return out
}
