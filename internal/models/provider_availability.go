package models

import "context"

// ProviderAvailability reports which model providers the process is configured
// to reach.
//
// A model whose provider has no key is not merely degraded — the request fails
// outright (see the "%s model %q requested but %s_API_KEY is not configured"
// errors in internal/agent). Listing it anyway offers the user a choice the
// server cannot honour, and the failure arrives only after they have written
// and sent a message.
type ProviderAvailability struct {
	// keyed is the set of providers with a configured credential.
	keyed map[ModelProvider]bool
	// filter is false when provider credentials do not determine which models
	// work, in which case every model is reported available.
	filter bool
	// live, when set, is consulted instead of the static entries. Keys belong
	// to accounts and can be set at runtime, so availability is a question
	// about the caller rather than about the process: someone who has just
	// entered a key expects their models to appear, and someone who has not
	// should not be offered models that will fail for them.
	live func(context.Context, ModelProvider) bool
}

// WithLiveCredentials returns a copy that asks configured() about the caller
// rather than using the boot-time values.
func (p ProviderAvailability) WithLiveCredentials(configured func(context.Context, ModelProvider) bool) ProviderAvailability {
	p.live = configured
	return p
}

// NewProviderAvailability builds availability from the configured keys.
//
// vendorBackend must be false under LLM_BACKEND=mock or local (ADR 0x018):
// those backends serve every model from the mock adapter or a single local
// server without consulting provider keys, so filtering by key there would
// hide models that in fact work. The hermetic e2e suites rely on this — they
// configure only OPENAI_API_KEY while exercising the full catalog.
func NewProviderAvailability(vendorBackend bool, openAI, anthropic, zai, gemini, mistral, deepSeek, qwen, xiaomi string) ProviderAvailability {
	return ProviderAvailability{
		filter: vendorBackend,
		keyed: map[ModelProvider]bool{
			ModelProviderOpenAI:    openAI != "",
			ModelProviderAnthropic: anthropic != "",
			ModelProviderZAI:       zai != "",
			ModelProviderGoogle:    gemini != "",
			ModelProviderMistral:   mistral != "",
			ModelProviderDeepSeek:  deepSeek != "",
			ModelProviderQwen:      qwen != "",
			ModelProviderXiaomi:    xiaomi != "",
		},
	}
}

// Available reports whether the caller can actually use models from this
// provider.
func (p ProviderAvailability) Available(ctx context.Context, provider ModelProvider) bool {
	if !p.filter {
		return true
	}
	if p.live != nil {
		return p.live(ctx, provider)
	}
	return p.keyed[provider]
}

// FilterUsable returns only the models whose provider is reachable. It returns
// a non-nil empty slice rather than nil so callers encoding straight to JSON
// emit [] instead of null.
func (p ProviderAvailability) FilterUsable(ctx context.Context, in []*Model) []*Model {
	out := make([]*Model, 0, len(in))
	for _, m := range in {
		if m == nil {
			continue
		}
		if p.Available(ctx, ProviderForModel(m.Provider, m.Name)) {
			out = append(out, m)
		}
	}
	return out
}

// catalogProviders is every provider the seeded model catalog uses. It is
// derived from AvailableModels rather than hand-listed so a provider added to
// the catalog appears in the integrations screen without a second edit.
func CatalogProviders() []ModelProvider {
	seen := make(map[ModelProvider]bool, len(AvailableModels))
	out := make([]ModelProvider, 0, 4)
	for _, m := range AvailableModels {
		p := ProviderForModel(string(m.Provider), m.Name)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// IsCatalogProvider reports whether name is a provider the catalog uses. Used
// to reject unknown providers on write rather than storing a key nothing will
// ever read.
func IsCatalogProvider(name string) bool {
	for _, p := range CatalogProviders() {
		if string(p) == name {
			return true
		}
	}
	return false
}

// SupportsPerAccountKey reports whether a key stored against an account is
// actually used for that account's requests.
//
// True for every provider in the catalog now. It was OpenAI-only while two
// things were true — the shared transport rewrote credentials for the OpenAI
// host alone, and every other provider's client was built at boot from an
// environment variable and left nil without one — so a key stored for anything
// else would have been accepted and then ignored.
//
// Kept rather than deleted because it is the honest place to say no again: a
// provider added to the catalog whose credential the transport does not yet
// carry should return false here rather than accept a key it will not use.
func SupportsPerAccountKey(provider string) bool {
	return IsCatalogProvider(provider)
}
