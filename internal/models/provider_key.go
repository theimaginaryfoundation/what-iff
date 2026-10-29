package models

import "time"

// ModelProviders lists every provider a model can be routed through.
var ModelProviders = []ModelProvider{
	ModelProviderOpenAI,
	ModelProviderAnthropic,
	ModelProviderZAI,
	ModelProviderGoogle,
	ModelProviderMistral,
	ModelProviderDeepSeek,
	ModelProviderQwen,
	ModelProviderXiaomi,
}

// Known reports whether p is one of ModelProviders.
func (p ModelProvider) Known() bool {
	for _, known := range ModelProviders {
		if p == known {
			return true
		}
	}
	return false
}

// providerNames are the names a person would know a provider by.
var providerNames = map[ModelProvider]string{
	ModelProviderOpenAI:    "OpenAI",
	ModelProviderAnthropic: "Anthropic",
	ModelProviderZAI:       "z.ai",
	ModelProviderGoogle:    "Google Gemini",
	ModelProviderMistral:   "Mistral",
	ModelProviderDeepSeek:  "DeepSeek",
	ModelProviderQwen:      "Qwen",
	ModelProviderXiaomi:    "Xiaomi MiMo",
}

// DisplayName is the name a person would know p by, or p itself when it is not
// a known provider.
func (p ModelProvider) DisplayName() string {
	if name, ok := providerNames[p]; ok {
		return name
	}
	return string(p)
}

// ProviderKeySummary describes a stored provider key without revealing it.
type ProviderKeySummary struct {
	Provider ModelProvider `json:"provider"`
	// Hint is the key's last four characters.
	Hint      string    `json:"hint"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Where provider keys come from, as reported by ProviderAccess.KeysFrom.
const (
	KeysFromDeployment = "deployment"
	KeysFromAccount    = "account"
)

// ProviderAccess is what the requesting account can reach: where its provider
// keys come from, and which providers it has a key for right now.
type ProviderAccess struct {
	KeysFrom  string          `json:"keys_from"`
	Providers []ModelProvider `json:"providers"`
}
