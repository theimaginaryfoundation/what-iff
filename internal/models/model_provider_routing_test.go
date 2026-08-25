package models

import "testing"

func TestUsesAnthropicMessagesAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{name: "native anthropic", provider: "anthropic", model: "claude-sonnet-4-6", want: true},
		{name: "zai glm via provider", provider: "zai", model: "glm-5.2", want: true},
		{name: "zai glm without provider", provider: "", model: "glm-5.2", want: true},
		{name: "openai", provider: "openai", model: "gpt-5.1", want: false},
		{name: "gemini is not anthropic-messages", provider: "google", model: "gemini-3.5", want: false},
		{name: "mistral is not anthropic-messages", provider: "mistral", model: "mistral-large", want: false},
		{name: "unknown", provider: "", model: "nope", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := UsesAnthropicMessagesAPI(tt.provider, tt.model); got != tt.want {
				t.Fatalf("UsesAnthropicMessagesAPI(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestIsZAIModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{provider: "zai", model: "glm-5.2", want: true},
		{provider: "", model: "glm-5.2", want: true},
		{provider: "anthropic", model: "claude-sonnet-4-6", want: false},
		{provider: "google", model: "gemini-3.5", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()
			if got := IsZAIModel(tt.provider, tt.model); got != tt.want {
				t.Fatalf("IsZAIModel(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestIsGeminiModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{provider: "google", model: "gemini-3.5", want: true},
		{provider: "google", model: "gemini-3.5-flash", want: true},
		{provider: "", model: "gemini-3.5-flash", want: true},
		{provider: "openai", model: "gpt-5.1", want: false},
		{provider: "zai", model: "glm-5.2", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()
			if got := IsGeminiModel(tt.provider, tt.model); got != tt.want {
				t.Fatalf("IsGeminiModel(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestUsesOpenAIChatCompletionsAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{provider: "google", model: "gemini-3.5", want: true},
		{provider: "mistral", model: "mistral-large-latest", want: true},
		{provider: "deepseek", model: "deepseek-chat", want: true},
		{provider: "qwen", model: "qwen-plus", want: true},
		{provider: "xiaomi", model: "mimo-v2.5-pro", want: true},
		{provider: "openai", model: "gpt-5.1", want: false},
		{provider: "anthropic", model: "claude-sonnet-4-6", want: false},
		{provider: "", model: "custom-model", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()
			if got := UsesOpenAIChatCompletionsAPI(tt.provider, tt.model); got != tt.want {
				t.Fatalf("UsesOpenAIChatCompletionsAPI(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestChatCompletionsSupportsVision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{provider: "google", model: "gemini-3.5-flash", want: true},
		{provider: "qwen", model: "qwen3.7-plus", want: true},
		{provider: "qwen", model: "qwen-plus", want: false},
		{provider: "mistral", model: "mistral-medium-2508", want: true},
		{provider: "mistral", model: "mistral-medium-3.5", want: true},
		{provider: "mistral", model: "codestral-latest", want: false},
		{provider: "deepseek", model: "deepseek-chat", want: false},
		{provider: "xiaomi", model: "mimo-v2.5-pro", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()
			if got := ChatCompletionsSupportsVision(tt.provider, tt.model); got != tt.want {
				t.Fatalf("ChatCompletionsSupportsVision(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

// IsAnthropicModel must remain native-only (not GLM) so Anthropic-only features
// (web search, beta MCP, prompt caching) are not enabled for z.ai.
func TestIsAnthropicModel_ExcludesZAI(t *testing.T) {
	t.Parallel()
	if IsAnthropicModel("zai", "glm-5.2") {
		t.Fatal("IsAnthropicModel should be false for z.ai GLM models")
	}
	if !IsNativeAnthropicModel("anthropic", "claude-sonnet-4-6") {
		t.Fatal("IsNativeAnthropicModel should be true for native Anthropic models")
	}
}
