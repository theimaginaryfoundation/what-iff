package server

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// providerEndpoint is where one provider's requests go and how it expects its
// credential on the wire.
type providerEndpoint struct {
	provider appmodels.ModelProvider
	baseURL  string
	style    provider.CredentialStyle
	// pathScoped limits the endpoint to requests under baseURL's path; see
	// provider.CredentialRule.PathScoped.
	pathScoped bool
}

// providerEndpoints lists every provider the process can reach.
//
// The endpoint here is the same one used to build that provider's client, so
// pointing a provider at a proxy moves both the request and the rule that
// credentials it. Anything absent from this list simply carries whatever
// credential its client was built with.
func (c *Config) providerEndpoints() []providerEndpoint {
	orDefault := func(configured, fallback string) string {
		if configured != "" {
			return configured
		}
		return fallback
	}
	return []providerEndpoint{
		{appmodels.ModelProviderOpenAI, provider.DefaultOpenAIBaseURL, provider.BearerAuthorization, false},
		{appmodels.ModelProviderAnthropic, provider.DefaultAnthropicBaseURL, provider.AnthropicAPIKey, false},
		// z.ai speaks the Anthropic Messages API, so it takes the Anthropic
		// header with its own key — which is precisely why the rule has to be
		// scoped to a host rather than to a wire format.
		{appmodels.ModelProviderZAI, orDefault(c.ZAIBaseURL, provider.DefaultZAIBaseURL), provider.AnthropicAPIKey, false},
		{appmodels.ModelProviderGoogle, orDefault(c.GeminiBaseURL, provider.DefaultGeminiBaseURL), provider.BearerAuthorization, false},
		// Gemini's native API (image generation) shares the host but takes the key
		// as x-goog-api-key, and refuses a request that also carries it as Bearer.
		{appmodels.ModelProviderGoogle, provider.GeminiNativeBaseURL(c.GeminiBaseURL) + "/models", provider.GoogleAPIKey, true},
		{appmodels.ModelProviderMistral, orDefault(c.MistralBaseURL, provider.DefaultMistralBaseURL), provider.BearerAuthorization, false},
		{appmodels.ModelProviderDeepSeek, orDefault(c.DeepSeekBaseURL, provider.DefaultDeepSeekBaseURL), provider.BearerAuthorization, false},
		{appmodels.ModelProviderQwen, orDefault(c.QwenBaseURL, provider.DefaultQwenBaseURL), provider.BearerAuthorization, false},
		{appmodels.ModelProviderXiaomi, orDefault(c.XiaomiBaseURL, provider.DefaultXiaomiBaseURL), provider.BearerAuthorization, false},
	}
}

// builtinProviderURLs is the address each built-in provider is reached at,
// from the same endpoint list its client and credential rule use.
func (c *Config) builtinProviderURLs() map[appmodels.ModelProvider]string {
	out := make(map[appmodels.ModelProvider]string)
	for _, e := range c.providerEndpoints() {
		if !e.pathScoped {
			out[e.provider] = e.baseURL
		}
	}
	return out
}

// configuredProviderKeys is the credentials this deployment was started with.
func (c *Config) configuredProviderKeys() map[appmodels.ModelProvider]string {
	return map[appmodels.ModelProvider]string{
		appmodels.ModelProviderOpenAI:    c.OpenAIKey,
		appmodels.ModelProviderAnthropic: c.AnthropicKey,
		appmodels.ModelProviderZAI:       c.ZAIKey,
		appmodels.ModelProviderGoogle:    c.GeminiKey,
		appmodels.ModelProviderMistral:   c.MistralKey,
		appmodels.ModelProviderDeepSeek:  c.DeepSeekKey,
		appmodels.ModelProviderQwen:      c.QwenKey,
		appmodels.ModelProviderXiaomi:    c.XiaomiKey,
	}
}

// credentialRules turns the endpoint list into per-host rules that ask the
// resolver for a credential on every outgoing request.
func credentialRules(endpoints []providerEndpoint, resolver providerkeys.Resolver) []provider.CredentialRule {
	rules := make([]provider.CredentialRule, 0, len(endpoints))
	for _, e := range endpoints {
		rules = append(rules, provider.CredentialRule{
			Provider:   e.provider,
			BaseURL:    e.baseURL,
			Style:      e.style,
			PathScoped: e.pathScoped,
			Resolve:    func(ctx context.Context) string { return resolver.Key(ctx, e.provider) },
		})
	}
	return rules
}

// imageGenerationProviders lists the image vendors the caller holds a key for,
// in the settings UI's order. Keys may belong to accounts, so it is asked per
// request; under the deployment resolver every caller gets the same list.
func imageGenerationProviders(ctx context.Context, resolver providerkeys.Resolver) []string {
	var out []string
	for _, p := range provider.ImageGenerationProviders {
		if resolver.Key(ctx, p) != "" {
			out = append(out, string(p))
		}
	}
	return out
}
