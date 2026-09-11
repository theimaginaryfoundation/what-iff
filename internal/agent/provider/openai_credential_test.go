package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
)

type capturingRT struct{ got http.Header }

func (c *capturingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c.got = req.Header.Clone()
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func do(t *testing.T, client *http.Client, url, authHeader string) http.Header {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	return client.Transport.(*openAICredentialTransport).base.(*capturingRT).got
}

func TestInjectsResolvedKeyForOpenAIHost(t *testing.T) {
	client := OpenAICredentialHTTPClient(StaticOpenAIKey("sk-resolved"), &http.Client{Transport: &capturingRT{}})
	if got := do(t, client, "https://api.openai.com/v1/models", "Bearer sk-stale").Get("Authorization"); got != "Bearer sk-resolved" {
		t.Fatalf("want the resolved key, got %q", got)
	}
}

// Two accounts on one instance must use their own credentials — this is the
// whole reason keys are per-account rather than per-deployment.
func TestResolvesPerActor(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	keys := map[uuid.UUID]string{alice: "sk-alice", bob: "sk-bob"}
	resolve := func(ctx context.Context) string {
		if id, ok := apicontext.UserIDFrom(ctx); ok {
			return keys[id]
		}
		return ""
	}
	client := OpenAICredentialHTTPClient(resolve, &http.Client{Transport: &capturingRT{}})

	for id, want := range map[uuid.UUID]string{alice: "Bearer sk-alice", bob: "Bearer sk-bob"} {
		req, err := http.NewRequestWithContext(apicontext.WithUserID(context.Background(), id),
			http.MethodGet, "https://api.openai.com/v1/models", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Do(req); err != nil {
			t.Fatal(err)
		}
		got := client.Transport.(*openAICredentialTransport).base.(*capturingRT).got.Get("Authorization")
		if got != want {
			t.Fatalf("actor %s: want %q, got %q", id, want, got)
		}
	}
}

// The shared provider client is also used by Gemini, Mistral, DeepSeek, Qwen,
// Xiaomi and z.ai, which speak the OpenAI wire format and send their own key in
// the same header. Rewriting theirs would swap in the wrong credential.
func TestLeavesOtherProviderHostsAlone(t *testing.T) {
	client := OpenAICredentialHTTPClient(StaticOpenAIKey("sk-openai"), &http.Client{Transport: &capturingRT{}})

	for _, url := range []string{
		"https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
		"https://api.deepseek.com/chat/completions",
		"https://open.bigmodel.cn/api/paas/v4/chat/completions",
	} {
		if got := do(t, client, url, "Bearer their-own-key").Get("Authorization"); got != "Bearer their-own-key" {
			t.Fatalf("%s: credential leaked across providers, got %q", url, got)
		}
	}
}

// With no key configured the request goes out untouched rather than with an
// empty bearer, so the failure is the provider's own 401 rather than a
// malformed header.
func TestNoKeyLeavesRequestUnchanged(t *testing.T) {
	client := OpenAICredentialHTTPClient(StaticOpenAIKey(""), &http.Client{Transport: &capturingRT{}})
	if got := do(t, client, "https://api.openai.com/v1/models", "Bearer boot-key").Get("Authorization"); got != "Bearer boot-key" {
		t.Fatalf("want the untouched header, got %q", got)
	}
}

// Wrapping must preserve the base client's own transport — under a non-vendor
// backend that transport is the deny-network one, and losing it would open
// egress the backend is supposed to forbid.
func TestPreservesBaseTransport(t *testing.T) {
	deny := DenyNetworkHTTPClient()
	wrapped := OpenAICredentialHTTPClient(StaticOpenAIKey("sk-x"), deny)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	if _, err := wrapped.Get(srv.URL); err == nil {
		t.Fatal("wrapped deny client allowed egress")
	}
}
