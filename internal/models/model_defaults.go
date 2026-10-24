package models

import "strings"

const (
	// DefaultModelName specifies which model from AvailableModels to use as the default
	DefaultModelName = "gpt-5.6-luna"

	// UtilityModelName is the model for short background work that is not the
	// assistant talking: naming a chat, picking an expression, selecting a mood,
	// answering a memory query, parsing a job schedule, and the first-chat
	// greeting.
	//
	// Keeping them on one constant makes retiring a model a single edit, and
	// keeps the greeting on the same provider as the rest of the default
	// experience rather than on one the install may have no key for.
	//
	// Assistant-facing generation does not use this. Personality generation, the
	// summarizer and image work each pick their own model for quality reasons.
	UtilityModelName = "gpt-5.6-luna"
)

// ModelProvider identifies which API backend a model is routed through.
type ModelProvider string

const (
	ModelProviderOpenAI    ModelProvider = "openai"
	ModelProviderAnthropic ModelProvider = "anthropic"
	// ModelProviderZAI routes through z.ai's Anthropic-compatible Messages API
	// (GLM models). It reuses the entire Claude rendering/adapter path with a
	// different base URL + API key; only Anthropic-native tool features
	// (web search, beta MCP, prompt caching) differ — see IsNativeAnthropicModel.
	ModelProviderZAI ModelProvider = "zai"
	// ModelProviderGoogle routes through Google's OpenAI-compatible Chat
	// Completions API (Gemini models). This is a distinct API family from the
	// OpenAI Responses API the rest of the app uses, so it has its own provider,
	// adapter, and renderer.
	ModelProviderGoogle ModelProvider = "google"
	// ModelProviderMistral routes through Mistral's OpenAI-compatible Chat
	// Completions API.
	ModelProviderMistral ModelProvider = "mistral"
	// ModelProviderDeepSeek routes through DeepSeek's OpenAI-compatible Chat
	// Completions API.
	ModelProviderDeepSeek ModelProvider = "deepseek"
	// ModelProviderQwen routes through Alibaba DashScope's OpenAI-compatible Chat
	// Completions API (Qwen models).
	ModelProviderQwen ModelProvider = "qwen"
	// ModelProviderXiaomi routes through Xiaomi MiMo's OpenAI-compatible Chat
	// Completions API.
	ModelProviderXiaomi ModelProvider = "xiaomi"
	// ModelProviderCheaperInference is the Cheaper Inference relay. It is a route,
	// not a catalog provider: no model row names it, and it is absent from
	// ModelProviders. It exists so the relay's credential resolves like any other
	// (ADR 0x029).
	ModelProviderCheaperInference ModelProvider = "cheaperinference"
)

// ModelConfig represents a model's configuration
type ModelConfig struct {
	Name        string
	DisplayName string
	Description string
	ToolSupport bool
	// VisionSupport marks models that accept image input; images are stripped
	// from the context for models without it.
	VisionSupport bool
	Provider      ModelProvider
}

// AvailableModels is the complete list of models that will be seeded into the database.
// The default model is determined by the DefaultModelName constant above.
var AvailableModels = []ModelConfig{
	{
		Name:          "gpt-5.1",
		DisplayName:   "GPT-5.1",
		Description:   "OpenAI's model for coding and agentic tasks, with configurable reasoning effort.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.4",
		DisplayName:   "GPT-5.4",
		Description:   "OpenAI's model for agentic, coding and professional work at scale.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.6-luna",
		DisplayName:   "GPT-5.6 Luna",
		Description:   "OpenAI's low-cost GPT-5.6 model for high-volume work, the counterpart of a GPT-5 nano.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-4o",
		DisplayName:   "GPT-4o",
		Description:   "OpenAI's multimodal GPT-4o, strong at vision and non-English text.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5",
		DisplayName:   "GPT-5",
		Description:   "OpenAI's GPT-5, combining reasoning and conversation. 272K-token input, 128K-token output.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5-mini",
		DisplayName:   "GPT-5 Mini",
		Description:   "A faster, lower-cost GPT-5 for cost-sensitive work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-4.1-mini",
		DisplayName:   "GPT-4.1 Mini",
		Description:   "OpenAI's GPT-4.1 Mini, a general-purpose chat model.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-4o-mini",
		DisplayName:   "GPT-4o Mini",
		Description:   "A smaller, faster, lower-cost GPT-4o for high-volume work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},

	{
		Name:          "gpt-6-sol",
		DisplayName:   "GPT-6 Sol",
		Description:   "OpenAI's frontier GPT-6 model, for complex coding and agentic work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-6-luna",
		DisplayName:   "GPT-6 Luna",
		Description:   "OpenAI's efficient GPT-6 model, for focused, high-volume tasks.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.6-sol",
		DisplayName:   "GPT-5.6 Sol",
		Description:   "OpenAI's frontier GPT-5.6 model, the counterpart of an unsuffixed GPT-5 model.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.6-terra",
		DisplayName:   "GPT-5.6 Terra",
		Description:   "OpenAI's GPT-5.6 model balancing intelligence and cost, the counterpart of a GPT-5 mini.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.5",
		DisplayName:   "GPT-5.5",
		Description:   "OpenAI's frontier model for complex professional work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.4-mini",
		DisplayName:   "GPT-5.4 Mini",
		Description:   "A faster, more efficient GPT-5.4 for high-volume work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5.4-nano",
		DisplayName:   "GPT-5.4 Nano",
		Description:   "The smallest GPT-5.4, for work where speed and cost matter most, such as classification and extraction.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	{
		Name:          "gpt-5-nano",
		DisplayName:   "GPT-5 Nano",
		Description:   "The smallest, fastest GPT-5, for high-throughput work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderOpenAI,
	},
	// Anthropic Claude models
	/*
				Commenting out Opus 4.6 as it is more expensive than our existing models so it needs better rate limiting before GA
				{
					Name:        "claude-opus-4-6",
					DisplayName: "Claude Opus 4.6",
					Description: "Anthropic's most intelligent model. Exceptional at complex analysis, coding, and nuanced reasoning with a 200K token context window.",
					ToolSupport: true,
		VisionSupport: true,
					Provider:    ModelProviderAnthropic,
				},
	*/
	{
		Name:          "claude-opus-5",
		DisplayName:   "Opus 5",
		Description:   "Anthropic's most capable Opus-tier model. Adaptive thinking, strong at complex analysis and coding, with a 1M token context window.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-sonnet-5",
		DisplayName:   "Sonnet 5",
		Description:   "Anthropic's Sonnet 5, balancing speed and intelligence.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-sonnet-4-6",
		DisplayName:   "Sonnet 4.6",
		Description:   "Anthropic's Sonnet 4.6, balancing speed and intelligence, with extended thinking.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-haiku-4-5",
		DisplayName:   "Haiku 4.5",
		Description:   "Anthropic's Haiku 4.5, a compact model for fast, latency-sensitive work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},

	{
		Name:          "claude-opus-4-8",
		DisplayName:   "Opus 4.8",
		Description:   "Anthropic's Opus 4.8, for complex reasoning and agentic coding.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-opus-4-7",
		DisplayName:   "Opus 4.7",
		Description:   "Anthropic's Opus 4.7, for complex reasoning and agentic coding.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-opus-4-5",
		DisplayName:   "Opus 4.5",
		Description:   "Anthropic's Opus 4.5, for complex reasoning and agentic coding.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-sonnet-5-5",
		DisplayName:   "Sonnet 5.5",
		Description:   "Anthropic's Sonnet 5.5, balancing speed and intelligence.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-sonnet-4-5",
		DisplayName:   "Sonnet 4.5",
		Description:   "Anthropic's Sonnet 4.5, balancing speed and intelligence, with extended thinking.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	{
		Name:          "claude-haiku-5-5",
		DisplayName:   "Haiku 5.5",
		Description:   "Anthropic's Haiku 5.5, for fast, high-volume work such as classification, routing and extraction.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderAnthropic,
	},
	// z.ai GLM models (routed through z.ai's Anthropic-compatible Messages API).
	{
		Name:          "glm-5.3",
		DisplayName:   "GLM 5.3",
		Description:   "z.ai's flagship GLM model, stronger than GLM-5.2 at complex programming and long-horizon work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderZAI,
	},
	{
		Name:          "glm-5.2",
		DisplayName:   "GLM-5.2",
		Description:   "z.ai's low-cost, general-purpose GLM model.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderZAI,
	},

	{
		Name:          "glm-5.3-flash",
		DisplayName:   "GLM 5.3 Flash",
		Description:   "z.ai's low-cost, natively multimodal GLM-5 model.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderZAI,
	},
	// Google Gemini models (routed through Google's OpenAI-compatible Chat Completions API).
	{
		Name:          "gemini-3.8-flash",
		DisplayName:   "Gemini 3.8 Flash",
		Description:   "Google's most capable Flash model, for long-horizon agentic work at Flash speed and cost.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderGoogle,
	},
	{
		Name:          "gemini-3.5-flash",
		DisplayName:   "Gemini 3.5 Flash",
		Description:   "Google's fast, low-cost Gemini model for high-volume conversation.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderGoogle,
	},
	{
		Name:          "gemini-3.5",
		DisplayName:   "Gemini 3.5",
		Description:   "Google's flagship Gemini model balancing intelligence and cost for general-purpose conversation.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderGoogle,
	},
	{
		Name:          "gemini-3.7-flash",
		DisplayName:   "Gemini 3.7 Flash",
		Description:   "Google's Gemini 3.7 Flash, a natively multimodal reasoning model.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderGoogle,
	},
	{
		Name:          "gemini-3.1-pro-preview",
		DisplayName:   "Gemini 3.1 Pro",
		Description:   "A preview of Google's Gemini 3.1 Pro, for multimodal understanding and agentic work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderGoogle,
	},

	// Mistral models (routed through Mistral's OpenAI-compatible Chat Completions API).
	{
		Name:          "mistral-medium-3-5",
		DisplayName:   "Mistral 3.5 Medium",
		Description:   "Mistral's frontier multimodal model for agentic and coding work, released with open weights.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderMistral,
	},

	// DeepSeek models (routed through DeepSeek's OpenAI-compatible Chat Completions API).
	{
		Name:          "deepseek-v4-pro",
		DisplayName:   "DeepSeek V4 Pro",
		Description:   "DeepSeek's largest model. Text only.",
		ToolSupport:   true,
		VisionSupport: false,
		Provider:      ModelProviderDeepSeek,
	},
	{
		Name:          "deepseek-flash",
		DisplayName:   "DeepSeek Flash",
		Description:   "DeepSeek's small, fast V4.1 Flash model, with image understanding.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderDeepSeek,
	},

	// Qwen models (routed through Alibaba DashScope's OpenAI-compatible Chat Completions API).
	{
		Name:          "qwen3.7-plus",
		DisplayName:   "Qwen 3.7 Plus",
		Description:   "Alibaba's cost-effective Qwen 3.7 model, with vision, tool use and agent capabilities.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderQwen,
	},

	// Xiaomi MiMo models (routed through Xiaomi MiMo's OpenAI-compatible Chat Completions API).
	{
		Name:          "mimo-v2.6-pro",
		DisplayName:   "MiMo v2.6 Pro",
		Description:   "Xiaomi's flagship MiMo model.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderXiaomi,
	},
	{
		Name:          "mimo-v2.6-flash",
		DisplayName:   "MiMo v2.6 Flash",
		Description:   "Xiaomi's low-cost MiMo reasoning model, for frequent, large-scale work.",
		ToolSupport:   true,
		VisionSupport: true,
		Provider:      ModelProviderXiaomi,
	},
}

// GetDefaultModel returns the ModelConfig for the default model
func GetDefaultModel() *ModelConfig {
	for i := range AvailableModels {
		if AvailableModels[i].Name == DefaultModelName {
			return &AvailableModels[i]
		}
	}
	// Fallback to first model if default not found
	if len(AvailableModels) > 0 {
		return &AvailableModels[0]
	}
	return nil
}

// CatalogModel returns the seed-catalog entry for name, or nil when the name
// is not in AvailableModels.
func CatalogModel(name string) *ModelConfig {
	for i := range AvailableModels {
		if AvailableModels[i].Name == name {
			return &AvailableModels[i]
		}
	}
	return nil
}

// IsClaudeModel reports whether the named model is routed through the Anthropic
// provider according to AvailableModels. Returns false for unrecognised names.
func IsClaudeModel(name string) bool {
	for _, m := range AvailableModels {
		if m.Name == name {
			return m.Provider == ModelProviderAnthropic
		}
	}
	return false
}

// ProviderForModel resolves the effective provider for a (provider, name) pair.
// The DB-row provider takes precedence; otherwise the seed catalog is consulted.
// Returns empty string when unknown.
func ProviderForModel(provider, name string) ModelProvider {
	return providerForModel(provider, name)
}

// providerForModel resolves the effective provider for a (provider, name) pair.
// The DB-row provider takes precedence; otherwise the seed catalog is consulted.
// Returns empty string when unknown.
func providerForModel(provider, name string) ModelProvider {
	switch p := ModelProvider(strings.TrimSpace(strings.ToLower(provider))); p {
	case ModelProviderAnthropic, ModelProviderOpenAI, ModelProviderZAI, ModelProviderGoogle,
		ModelProviderMistral, ModelProviderDeepSeek, ModelProviderQwen, ModelProviderXiaomi:
		return p
	}
	for _, m := range AvailableModels {
		if m.Name == name {
			return m.Provider
		}
	}
	return ""
}

// IsAnthropicModel reports whether a model is served by Anthropic's own API
// (native Anthropic). This gates Anthropic-native tool features (native web
// search, beta MCP). z.ai GLM models share the Messages API wire format but are
// NOT native Anthropic — use UsesAnthropicMessagesAPI for rendering/adapter routing.
// When provider is set (from the DB row), it takes precedence over the seed catalog.
func IsAnthropicModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderAnthropic
}

// IsNativeAnthropicModel is an explicit alias for IsAnthropicModel used at call
// sites that gate Anthropic-only features (web search, beta MCP, prompt caching)
// so intent is clear alongside UsesAnthropicMessagesAPI.
func IsNativeAnthropicModel(provider, name string) bool {
	return IsAnthropicModel(provider, name)
}

// UsesAnthropicMessagesAPI reports whether a model is driven through the
// Anthropic Messages API wire format (rendering + ClaudeProvider + ClaudeAdapter).
// True for native Anthropic models AND z.ai GLM models (z.ai exposes an
// Anthropic-compatible endpoint). Use this for path/rendering routing.
func UsesAnthropicMessagesAPI(provider, name string) bool {
	switch providerForModel(provider, name) {
	case ModelProviderAnthropic, ModelProviderZAI:
		return true
	default:
		return false
	}
}

// IsZAIModel reports whether a model routes through z.ai (GLM).
func IsZAIModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderZAI
}

// IsGeminiModel reports whether a model routes through Google's Gemini
// (OpenAI-compatible Chat Completions) API.
func IsGeminiModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderGoogle
}

// IsMistralModel reports whether a model routes through Mistral's OpenAI-compatible API.
func IsMistralModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderMistral
}

// IsDeepSeekModel reports whether a model routes through DeepSeek's OpenAI-compatible API.
func IsDeepSeekModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderDeepSeek
}

// IsQwenModel reports whether a model routes through Qwen's OpenAI-compatible API.
func IsQwenModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderQwen
}

// IsXiaomiModel reports whether a model routes through Xiaomi MiMo's OpenAI-compatible API.
func IsXiaomiModel(provider, name string) bool {
	return providerForModel(provider, name) == ModelProviderXiaomi
}

// UsesOpenAIChatCompletionsAPI reports whether a model is driven through an
// OpenAI-compatible Chat Completions endpoint (Gemini, Mistral, DeepSeek, Qwen, Xiaomi).
// These share message rendering but may use different adapters and API keys.
func UsesOpenAIChatCompletionsAPI(provider, name string) bool {
	switch providerForModel(provider, name) {
	case ModelProviderGoogle, ModelProviderMistral, ModelProviderDeepSeek, ModelProviderQwen, ModelProviderXiaomi:
		return true
	default:
		return false
	}
}

// IsExperimentalProvider reports whether a provider is gated behind the per-user
// enable_experimental_models flag. Currently none — Gemini/Qwen/GLM/Mistral/MiMo/
// DeepSeek graduated. Re-add dogfood vendors in IsExperimentalModelRecord.
func IsExperimentalProvider(provider string) bool {
	return IsExperimentalModelRecord(provider, "")
}

// IsExperimentalModelRecord reports whether a model requires experimental access,
// using the DB provider when set and falling back to name heuristics when not.
func IsExperimentalModelRecord(provider, name string) bool {
	switch providerForModel(provider, name) {
	// Empty on purpose: no providers are gated right now.
	// Add new vendors here when dogfooding behind enable_experimental_models.
	default:
		return false
	}
}

// IsExperimentalModel reports whether a catalog model requires experimental access.
func IsExperimentalModel(m *Model) bool {
	return m != nil && IsExperimentalModelRecord(m.Provider, m.Name)
}
