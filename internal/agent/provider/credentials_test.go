package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
)

type capturingRT struct{ got http.Header }

func (c *capturingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c.got = req.Header.Clone()
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func send(t *testing.T, c *http.Client, url string, seed map[string]string, ctx context.Context) http.Header {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range seed {
		req.Header.Set(k, v)
	}
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	return c.Transport.(*credentialTransport).base.(*capturingRT).got
}

func rules() []CredentialRule {
	return []CredentialRule{
		{BaseURL: DefaultOpenAIBaseURL, Style: BearerAuthorization, Resolve: StaticKey("sk-openai")},
		{BaseURL: DefaultAnthropicBaseURL, Style: AnthropicAPIKey, Resolve: StaticKey("sk-ant")},
		{BaseURL: DefaultZAIBaseURL, Style: AnthropicAPIKey, Resolve: StaticKey("sk-zai")},
		{BaseURL: DefaultGeminiBaseURL, Style: BearerAuthorization, Resolve: StaticKey("sk-gemini")},
		{BaseURL: DefaultMistralBaseURL, Style: BearerAuthorization, Resolve: StaticKey("sk-mistral")},
	}
}

// Each provider gets its own credential, in the header that provider expects.
// Anthropic and z.ai share a wire format but not a host, and the four
// OpenAI-compatible providers share a header but not a key — which is the
// whole reason a rule is scoped to a host.
func TestEachProviderGetsItsOwnCredential(t *testing.T) {
	for _, tc := range []struct{ name, url, header, want string }{
		{"openai", "https://api.openai.com/v1/responses", "Authorization", "Bearer sk-openai"},
		{"anthropic", "https://api.anthropic.com/v1/messages", "x-api-key", "sk-ant"},
		{"zai", "https://api.z.ai/api/anthropic/v1/messages", "x-api-key", "sk-zai"},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", "Authorization", "Bearer sk-gemini"},
		{"mistral", "https://api.mistral.ai/v1/chat/completions", "Authorization", "Bearer sk-mistral"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := CredentialHTTPClient(rules(), &http.Client{Transport: &capturingRT{}})
			got := send(t, c, tc.url, map[string]string{tc.header: "stale"}, context.Background()).Get(tc.header)
			if got != tc.want {
				t.Fatalf("%s: want %q, got %q", tc.name, tc.want, got)
			}
		})
	}
}

// Anthropic must not receive an Authorization header from a neighbouring rule,
// and OpenAI must not receive x-api-key.
func TestCredentialsDoNotBleedAcrossStyles(t *testing.T) {
	c := CredentialHTTPClient(rules(), &http.Client{Transport: &capturingRT{}})
	h := send(t, c, "https://api.anthropic.com/v1/messages", nil, context.Background())
	if h.Get("Authorization") != "" {
		t.Fatalf("anthropic received an Authorization header: %q", h.Get("Authorization"))
	}
	h = send(t, c, "https://api.openai.com/v1/responses", nil, context.Background())
	if h.Get("x-api-key") != "" {
		t.Fatalf("openai received x-api-key: %q", h.Get("x-api-key"))
	}
}

// A host nobody claimed is left exactly as the SDK built it.
func TestUnknownHostUntouched(t *testing.T) {
	c := CredentialHTTPClient(rules(), &http.Client{Transport: &capturingRT{}})
	h := send(t, c, "https://example.invalid/v1/x", map[string]string{"Authorization": "Bearer theirs"}, context.Background())
	if got := h.Get("Authorization"); got != "Bearer theirs" {
		t.Fatalf("unknown host was rewritten: %q", got)
	}
}

// The point of deriving the host from configuration: pointing a provider at a
// proxy moves the rule with it, instead of leaving a constant matching a host
// the client no longer talks to.
func TestHostFollowsConfiguredBaseURL(t *testing.T) {
	custom := "https://llm-proxy.internal.example/v1"
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: custom, Style: BearerAuthorization, Resolve: StaticKey("sk-proxied")},
	}, &http.Client{Transport: &capturingRT{}})

	if got := send(t, c, custom+"/chat/completions", nil, context.Background()).Get("Authorization"); got != "Bearer sk-proxied" {
		t.Fatalf("rule did not follow the configured base URL: %q", got)
	}
	// And it no longer applies to the default host.
	if got := send(t, c, "https://api.openai.com/v1/responses", nil, context.Background()).Get("Authorization"); got != "" {
		t.Fatalf("rule still applied to the default host: %q", got)
	}
}

// Credentials stay per-account across every provider, not just OpenAI.
func TestResolvesPerActorForAnyProvider(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	keys := map[uuid.UUID]string{alice: "sk-ant-alice", bob: "sk-ant-bob"}
	c := CredentialHTTPClient([]CredentialRule{{
		BaseURL: DefaultAnthropicBaseURL,
		Style:   AnthropicAPIKey,
		Resolve: func(ctx context.Context) string {
			if id, ok := apicontext.UserIDFrom(ctx); ok {
				return keys[id]
			}
			return ""
		},
	}}, &http.Client{Transport: &capturingRT{}})

	for id, want := range map[uuid.UUID]string{alice: "sk-ant-alice", bob: "sk-ant-bob"} {
		ctx := apicontext.WithUserID(context.Background(), id)
		if got := send(t, c, "https://api.anthropic.com/v1/messages", nil, ctx).Get("x-api-key"); got != want {
			t.Fatalf("actor %s: want %q, got %q", id, want, got)
		}
	}
}

// Wrapping preserves the base transport, so a non-vendor backend keeps its
// deny-network client.
func TestCredentialClientPreservesBaseTransport(t *testing.T) {
	wrapped := CredentialHTTPClient(rules(), DenyNetworkHTTPClient())
	if _, err := wrapped.Get("https://api.openai.com/v1/models"); err == nil {
		t.Fatal("wrapped deny client allowed egress")
	}
}

// With no credential for the caller the request goes out exactly as the SDK
// built it. An empty header would turn a clean provider 401 into a malformed
// one, and the provider's own error is the more useful failure.
func TestNoCredentialLeavesRequestUnchanged(t *testing.T) {
	c := CredentialHTTPClient([]CredentialRule{
		{BaseURL: DefaultOpenAIBaseURL, Style: BearerAuthorization, Resolve: StaticKey("")},
	}, &http.Client{Transport: &capturingRT{}})

	got := send(t, c, "https://api.openai.com/v1/responses",
		map[string]string{"Authorization": "Bearer boot-key"}, context.Background()).Get("Authorization")
	if got != "Bearer boot-key" {
		t.Fatalf("want the untouched header, got %q", got)
	}
}
