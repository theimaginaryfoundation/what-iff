package models

import "strings"

const (
	// DefaultModelName specifies which model from AvailableModels to use as the default
	DefaultModelName = "gpt-5.1"

	// LegacyFineTunedModelName is the old fine-tuned model that will be migrated
	LegacyFineTunedModelName = "ft:gpt-4.1-2025-04-14:sotherden-io:personal-agent:CFNpAx1S"
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
)

// ModelConfig represents a model's configuration
type ModelConfig struct {
	Name        string
	DisplayName string
	Description string
	ToolSupport bool
	Provider    ModelProvider
}

// AvailableModels is the complete list of models that will be seeded into the database.
// The default model is determined by the DefaultModelName constant above.
var AvailableModels = []ModelConfig{
	{
		Name:        "gpt-5.1",
		DisplayName: "GPT-5.1",
		Description: "GPT-5.1 is our flagship model for coding and agentic tasks with configurable reasoning and non-reasoning effort.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-5.4",
		DisplayName: "GPT-5.4",
		Description: "Best intelligence at scale for agentic, coding, and professional workflows.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-4o",
		DisplayName: "GPT-4o",
		Description: "OpenAI's flagship multimodal model with vision capabilities. Matches GPT-4 Turbo performance in text/coding with superior non-English and vision tasks.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-5",
		DisplayName: "GPT-5",
		Description: "OpenAI's latest flagship model combining reasoning and conversational capabilities. 272K token input, 128K token output.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-5-mini",
		DisplayName: "GPT-5 Mini",
		Description: "Faster, more affordable GPT-5 variant with same capabilities. Optimized for cost-sensitive applications.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-4.1-mini",
		DisplayName: "GPT-4.1 Mini",
		Description: "OpenAI GPT-4.1 Mini general-purpose chat model.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-4o-mini",
		DisplayName: "GPT-4o Mini",
		Description: "Smaller, faster, and more affordable version of GPT-4o. Excellent for high-volume tasks requiring good performance.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},
	{
		Name:        "gpt-5.1-nano",
		DisplayName: "GPT-5.1 Nano",
		Description: "Ultra-fast, lightweight GPT-5.1 variant for high-throughput applications. Best price-performance ratio.",
		ToolSupport: true,
		Provider:    ModelProviderOpenAI,
	},

	// Anthropic Claude models
	/*
		Commenting out Opus 4.6 as it is more expensive than our existing models so it needs better rate limiting before GA
		{
			Name:        "claude-opus-4-6",
			DisplayName: "Claude Opus 4.6",
			Description: "Anthropic's most intelligent model. Exceptional at complex analysis, coding, and nuanced reasoning with a 200K token context window.",
			ToolSupport: true,
			Provider:    ModelProviderAnthropic,
		},
	*/
	{
		Name:        "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Description: "Anthropic's best balance of speed and intelligence with extended thinking capabilities. Ideal for most tasks.",
		ToolSupport: true,
		Provider:    ModelProviderAnthropic,
	},
	{
		Name:        "claude-haiku-4-5",
		DisplayName: "Claude 4.5 Haiku",
		Description: "Anthropic's fastest and most compact Claude model. Best for high-volume, latency-sensitive tasks.",
		ToolSupport: true,
		Provider:    ModelProviderAnthropic,
	},

	// z.ai GLM models (routed through z.ai's Anthropic-compatible Messages API).
	{
		Name:        "glm-5.2",
		DisplayName: "GLM-5.2",
		Description: "z.ai's flagship GLM model. A strong, low-cost general-purpose model routed through z.ai's Anthropic-compatible API.",
		ToolSupport: true,
		Provider:    ModelProviderZAI,
	},

	// Google Gemini models (routed through Google's OpenAI-compatible Chat Completions API).
	{
		Name:        "gemini-3.5-flash",
		DisplayName: "Gemini 3.5 Flash",
		Description: "Google's fast, low-cost Gemini model. Optimized for high-volume, latency-sensitive conversational tasks.",
		ToolSupport: true,
		Provider:    ModelProviderGoogle,
	},
	{
		Name:        "gemini-3.5",
		DisplayName: "Gemini 3.5",
		Description: "Google's flagship Gemini model balancing intelligence and cost for general-purpose conversation.",
		ToolSupport: true,
		Provider:    ModelProviderGoogle,
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

// ChatCompletionsSupportsVision reports whether an OpenAI-compatible Chat Completions
// model accepts multimodal image input. Gemini (google) always does; Qwen and Mistral
// are matched by model-id heuristics. DeepSeek and MiMo are text-only today.
//
// TODO: replace heuristics with a per-model allows_images (or similar) DB flag when
// admin model management grows a vision capability field.
func ChatCompletionsSupportsVision(provider, name string) bool {
	switch providerForModel(provider, name) {
	case ModelProviderGoogle:
		return true
	case ModelProviderQwen:
		return qwenModelSupportsVision(name)
	case ModelProviderMistral:
		return mistralModelSupportsVision(name)
	default:
		return false
	}
}

func qwenModelSupportsVision(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	// Qwen 3.7/3.6 multimodal line (e.g. qwen3.7-plus).
	if strings.Contains(n, "qwen3.7") || strings.Contains(n, "qwen3.6") {
		return true
	}
	// qwen3.5-plus/flash support vision per DashScope docs; plain qwen-plus does not.
	if strings.HasPrefix(n, "qwen3.5-plus") || strings.HasPrefix(n, "qwen3.5-flash") {
		return true
	}
	if strings.Contains(n, "qwen-vl") || strings.Contains(n, "qwen3-vl") || strings.Contains(n, "qwen2.5-vl") {
		return true
	}
	return false
}

func mistralModelSupportsVision(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	// Medium/Large/Small 3.x+ and Ministral/Magistral lines are multimodal.
	for _, marker := range []string{
		"mistral-medium",
		"mistral-large",
		"mistral-small",
		"ministral",
		"magistral",
	} {
		if strings.Contains(n, marker) {
			return true
		}
	}
	return false
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
