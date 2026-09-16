package provider

import (
	"net/http"
	"strings"
	"sync/atomic"
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

// OpenAICredential is the process-wide OpenAI API key.
//
// The key cannot simply be swapped on the clients that use it: the SDK client
// value is copied into six independent holders (OpenAIProvider, the recall and
// memory tools, the file-chunk pipeline, the memory handler, and the plugin
// embedder), so there is no single field to reassign. Instead the key lives
// here and is applied per request by the transport below, which every one of
// those clients already shares. Updating it is one atomic store and takes
// effect on the next request, with no restart and no changes at any call site.
type OpenAICredential struct {
	key atomic.Pointer[string]
}

// NewOpenAICredential seeds the credential with the key configured at boot,
// which may be empty when the operator has not supplied one yet.
func NewOpenAICredential(key string) *OpenAICredential {
	c := &OpenAICredential{}
	c.Set(key)
	return c
}

// Set replaces the key. Safe to call concurrently with in-flight requests: a
// request reads the pointer once, so it uses either the old key or the new one,
// never a torn value.
func (c *OpenAICredential) Set(key string) {
	k := strings.TrimSpace(key)
	c.key.Store(&k)
}

// Get returns the current key, empty when none is configured.
func (c *OpenAICredential) Get() string {
	if c == nil {
		return ""
	}
	if k := c.key.Load(); k != nil {
		return *k
	}
	return ""
}

// Configured reports whether a key has been supplied.
func (c *OpenAICredential) Configured() bool { return c.Get() != "" }

type openAICredentialTransport struct {
	cred *OpenAICredential
	base http.RoundTripper
}

func (t *openAICredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.URL == nil || req.URL.Hostname() != openAIAPIHost {
		return base.RoundTrip(req)
	}
	if key := t.cred.Get(); key != "" {
		// RoundTrip must not modify the caller's request, so clone before
		// touching headers (net/http.RoundTripper contract).
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return base.RoundTrip(req)
}

// OpenAICredentialHTTPClient wraps base so that requests to the OpenAI API
// carry the credential's current key. Pass the result wherever the shared
// provider HTTP client is expected; every OpenAI-family SDK client built from
// it then follows key changes automatically.
//
// base may be nil, in which case http.DefaultTransport is used.
func OpenAICredentialHTTPClient(cred *OpenAICredential, base *http.Client) *http.Client {
	out := &http.Client{}
	if base != nil {
		*out = *base
	}
	var baseRT http.RoundTripper
	if base != nil {
		baseRT = base.Transport
	}
	out.Transport = &openAICredentialTransport{cred: cred, base: baseRT}
	return out
}
