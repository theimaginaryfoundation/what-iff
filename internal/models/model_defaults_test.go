package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsClaudeModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "claude sonnet", model: "claude-sonnet-4-6", want: true},
		{name: "claude haiku", model: "claude-haiku-4-5", want: true},
		{name: "openai model", model: "gpt-5.1", want: false},
		{name: "unknown model", model: "not-a-model", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsClaudeModel(tt.model); got != tt.want {
				t.Fatalf("IsClaudeModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestIsAnthropicModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{name: "db anthropic provider", provider: "anthropic", model: "claude-sonnet-4-5", want: true},
		{name: "db openai provider", provider: "openai", model: "claude-sonnet-4-5", want: false},
		{name: "seed catalog fallback", provider: "", model: "claude-sonnet-4-6", want: true},
		{name: "unknown without provider", provider: "", model: "custom-model", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsAnthropicModel(tt.provider, tt.model); got != tt.want {
				t.Fatalf("IsAnthropicModel(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestProviderForModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		model    string
		want     ModelProvider
	}{
		{provider: "zai", model: "glm-5.2", want: ModelProviderZAI},
		{provider: "openai", model: "glm-5.2", want: ModelProviderOpenAI},
		{provider: "google", model: "gemini-3.5-flash", want: ModelProviderGoogle},
		{provider: "", model: "glm-5.2", want: ModelProviderZAI},
		{provider: "", model: "gemini-3.5-flash", want: ModelProviderGoogle},
		{provider: "mistral", model: "mistral-large", want: ModelProviderMistral},
		{provider: "deepseek", model: "deepseek-chat", want: ModelProviderDeepSeek},
		{provider: "qwen", model: "qwen-plus", want: ModelProviderQwen},
		{provider: "xiaomi", model: "mimo-v2.5-pro", want: ModelProviderXiaomi},
		{provider: "", model: "custom-admin-model", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.model, func(t *testing.T) {
			t.Parallel()
			if got := ProviderForModel(tt.provider, tt.model); got != tt.want {
				t.Fatalf("ProviderForModel(%q, %q) = %q, want %q", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestAvailableModels_NoDuplicateNames(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, len(AvailableModels))
	for _, m := range AvailableModels {
		if m.Name == "" {
			t.Fatalf("model has empty name: %+v", m)
		}
		if _, exists := seen[m.Name]; exists {
			t.Fatalf("duplicate model name found: %s", m.Name)
		}
		seen[m.Name] = struct{}{}
	}
}

func TestIsExperimentalProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		want     bool
	}{
		// Graduated providers — no longer gated.
		{provider: "google", want: false},
		{provider: "zai", want: false},
		{provider: "mistral", want: false},
		{provider: "deepseek", want: false},
		{provider: "qwen", want: false},
		{provider: "xiaomi", want: false},
		{provider: "openai", want: false},
		{provider: "anthropic", want: false},
		{provider: "", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsExperimentalProvider(tt.provider))
		})
	}
}

func TestIsExperimentalModelRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{name: "explicit google graduated", provider: "google", model: "gemini-3.5-flash", want: false},
		{name: "catalog glm without provider graduated", provider: "", model: "glm-5.2", want: false},
		{name: "explicit zai graduated", provider: "zai", model: "glm-5.2", want: false},
		{name: "explicit mistral graduated", provider: "mistral", model: "mistral-large", want: false},
		{name: "admin model without provider", provider: "", model: "custom-model", want: false},
		{name: "openai model", provider: "openai", model: "gpt-5.1", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsExperimentalModelRecord(tt.provider, tt.model))
		})
	}
}
