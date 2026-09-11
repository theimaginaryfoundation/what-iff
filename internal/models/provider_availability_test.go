package models

import "testing"

func mdl(name, provider string) *Model {
	return &Model{Name: name, Provider: provider}
}

// A vendor backend with only an OpenAI key must not offer models it cannot
// serve: those requests fail with "<provider>_API_KEY is not configured" only
// after the user has sent a message.
func TestFilterUsableHidesProvidersWithoutKeys(t *testing.T) {
	p := NewProviderAvailability(true, "sk-test", "", "", "", "", "", "", "")
	got := p.FilterUsable([]*Model{
		mdl("gpt-5.1", "openai"),
		mdl("claude-sonnet-4-6", "anthropic"),
		mdl("gemini-3.5", "google"),
		mdl("glm-5.2", "zai"),
	})
	if len(got) != 1 || got[0].Name != "gpt-5.1" {
		t.Fatalf("want only gpt-5.1, got %v", names(got))
	}
}

// Adding a key must light its models up without any other change.
func TestFilterUsableIncludesNewlyKeyedProvider(t *testing.T) {
	p := NewProviderAvailability(true, "sk-test", "sk-ant", "", "", "", "", "", "")
	got := p.FilterUsable([]*Model{
		mdl("gpt-5.1", "openai"),
		mdl("claude-sonnet-4-6", "anthropic"),
		mdl("gemini-3.5", "google"),
	})
	if len(got) != 2 {
		t.Fatalf("want openai+anthropic, got %v", names(got))
	}
}

// ADR 0x018: mock and local backends serve every model without provider keys,
// so filtering there would hide models that work. The hermetic e2e suites
// configure only OPENAI_API_KEY while exercising the whole catalog.
func TestFilterUsableDoesNotFilterOnNonVendorBackend(t *testing.T) {
	p := NewProviderAvailability(false, "dummy-mock-key", "", "", "", "", "", "", "")
	in := []*Model{
		mdl("gpt-5.1", "openai"),
		mdl("claude-sonnet-4-6", "anthropic"),
		mdl("gemini-3.5", "google"),
	}
	if got := p.FilterUsable(in); len(got) != len(in) {
		t.Fatalf("non-vendor backend must not filter; got %v", names(got))
	}
}

// No keys at all must yield an empty list rather than nil, so the JSON body is
// [] and the client renders "no models" instead of failing to parse.
func TestFilterUsableWithNoKeysReturnsEmptyNotNil(t *testing.T) {
	p := NewProviderAvailability(true, "", "", "", "", "", "", "", "")
	got := p.FilterUsable([]*Model{mdl("gpt-5.1", "openai")})
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", names(got))
	}
}

func names(ms []*Model) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}
