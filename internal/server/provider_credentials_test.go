package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// Every rule must resolve its own provider's credential. If a rule's resolver
// captured the wrong provider, requests to one host would carry another
// provider's key.
func TestCredentialRules_EachRuleResolvesItsOwnProvider(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	endpoints := cfg.providerEndpoints()
	keys := make(map[appmodels.ModelProvider]string, len(endpoints))
	for _, e := range endpoints {
		keys[e.provider] = "key-for-" + string(e.provider)
	}

	rules := credentialRules(endpoints, providerkeys.NewDeploymentResolver(keys))

	require.Len(t, rules, len(endpoints))
	for i, e := range endpoints {
		require.Equal(t, "key-for-"+string(e.provider), rules[i].Resolve(context.Background()), "rule for %s", e.provider)
		require.Equal(t, e.baseURL, rules[i].BaseURL)
	}
}

// A configured base URL moves the rule with the request, so a provider pointed
// at a proxy still gets its own credential.
func TestProviderEndpoints_FollowConfiguredBaseURLs(t *testing.T) {
	t.Parallel()
	cfg := &Config{ZAIBaseURL: "https://llm-proxy.example/zai"}
	byProvider := map[appmodels.ModelProvider]providerEndpoint{}
	var scoped []providerEndpoint
	for _, e := range cfg.providerEndpoints() {
		if e.pathScoped {
			scoped = append(scoped, e)
			continue
		}
		byProvider[e.provider] = e
	}

	require.Equal(t, "https://llm-proxy.example/zai", byProvider[appmodels.ModelProviderZAI].baseURL)
	require.Equal(t, provider.DefaultGeminiBaseURL, byProvider[appmodels.ModelProviderGoogle].baseURL, "unset base URLs fall back to the provider default")
	require.Equal(t, provider.AnthropicAPIKey, byProvider[appmodels.ModelProviderZAI].style, "z.ai speaks the Anthropic Messages API")
	// The relay is credentialed by its own host, so a routed call never carries the
	// model vendor's key and the fallback never carries the relay's (ADR 0x029).
	require.Equal(t, provider.DefaultCheaperInferenceBaseURL, byProvider[appmodels.ModelProviderCheaperInference].baseURL)
	require.Equal(t, provider.BearerAuthorization, byProvider[appmodels.ModelProviderCheaperInference].style)

	require.Len(t, scoped, 1, "only Gemini's native API needs a path-scoped rule")
	require.Equal(t, appmodels.ModelProviderGoogle, scoped[0].provider)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", scoped[0].baseURL)
	require.Equal(t, provider.GoogleAPIKey, scoped[0].style)
}

func TestProviderEndpoints_GeminiNativeFollowsConfiguredBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &Config{GeminiBaseURL: "https://gemini-proxy.example/v1beta/openai/"}
	for _, e := range cfg.providerEndpoints() {
		if e.pathScoped {
			require.Equal(t, "https://gemini-proxy.example/v1beta/models", e.baseURL)
			return
		}
	}
	t.Fatal("no path-scoped Gemini endpoint")
}

func TestImageGenerationProviders_ListsOnlyConfiguredVendorsInOrder(t *testing.T) {
	t.Parallel()
	resolver := providerkeys.NewDeploymentResolver(map[appmodels.ModelProvider]string{
		appmodels.ModelProviderQwen:     "q",
		appmodels.ModelProviderOpenAI:   "o",
		appmodels.ModelProviderDeepSeek: "d", // no image API
	})
	require.Equal(t, []string{"openai", "qwen"}, imageGenerationProviders(context.Background(), resolver))
}

// Every rule names its provider, including path-scoped ones, so a request with
// no key fails with an error that says which key is missing.
func TestCredentialRulesNameTheirProvider(t *testing.T) {
	cfg := &Config{}
	endpoints := cfg.providerEndpoints()
	rules := credentialRules(endpoints, providerkeys.NewDeploymentResolver(nil))
	require.Len(t, rules, len(endpoints))
	for i, r := range rules {
		require.Equal(t, endpoints[i].provider, r.Provider, r.BaseURL)
		require.NotEmpty(t, r.Provider, r.BaseURL)
	}
}
