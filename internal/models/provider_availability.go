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
	// openAI, when set, is consulted instead of the static entry. OpenAI keys
	// belong to accounts and can be set at runtime, so availability is a
	// question about the caller rather than about the process: a user who has
	// just entered a key expects their models to appear, and a user who has
	// not should not be offered models that will fail for them.
	openAI func(context.Context) bool
}

// WithLiveOpenAI returns a copy that asks configured() about the caller rather
// than using the boot-time value.
func (p ProviderAvailability) WithLiveOpenAI(configured func(context.Context) bool) ProviderAvailability {
	p.openAI = configured
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
	if provider == ModelProviderOpenAI && p.openAI != nil {
		return p.openAI(ctx)
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
