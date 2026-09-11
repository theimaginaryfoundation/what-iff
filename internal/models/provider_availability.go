package models

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

// Available reports whether models from this provider can actually be used.
func (p ProviderAvailability) Available(provider ModelProvider) bool {
	if !p.filter {
		return true
	}
	return p.keyed[provider]
}

// FilterUsable returns only the models whose provider is reachable. It returns
// a non-nil empty slice rather than nil so callers encoding straight to JSON
// emit [] instead of null.
func (p ProviderAvailability) FilterUsable(in []*Model) []*Model {
	out := make([]*Model, 0, len(in))
	for _, m := range in {
		if m == nil {
			continue
		}
		if p.Available(ProviderForModel(m.Provider, m.Name)) {
			out = append(out, m)
		}
	}
	return out
}
