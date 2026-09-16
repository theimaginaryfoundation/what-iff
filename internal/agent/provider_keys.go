package agent

import (
	"context"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// providerLabel and providerEnvVar describe a provider in user-facing errors.
// Both halves matter: a credential can come from the account or from the
// deployment environment, so an error that names only one of them sends half
// of readers to the wrong place.
var providerLabels = map[models.ModelProvider]struct{ name, envVar string }{
	models.ModelProviderOpenAI:    {"OpenAI", "OPENAI_API_KEY"},
	models.ModelProviderAnthropic: {"Anthropic", "ANTHROPIC_API_KEY"},
	models.ModelProviderZAI:       {"z.ai", "ZAI_API_KEY"},
	models.ModelProviderGoogle:    {"Gemini", "GEMINI_API_KEY"},
	models.ModelProviderMistral:   {"Mistral", "MISTRAL_API_KEY"},
	models.ModelProviderDeepSeek:  {"DeepSeek", "DEEPSEEK_API_KEY"},
	models.ModelProviderQwen:      {"Qwen", "QWEN_API_KEY"},
	models.ModelProviderXiaomi:    {"Xiaomi", "XIAOMI_API_KEY"},
}

// requireProviderKey reports whether the account behind ctx can use p, and
// returns an actionable error when it cannot.
//
// This replaces checking whether a provider client is nil. That test answered
// a question decided once at startup — did the process have an environment
// variable — which stopped being the right question when keys became something
// an account supplies. Two people on one instance can now differ, and only a
// per-request check can say so.
//
// A nil registry means provider keys are not in play at all (mock and local
// backends, and tests), in which case this must not block: those backends
// serve every model without credentials.
func (a *Agent) requireProviderKey(ctx context.Context, p models.ModelProvider, modelName string) error {
	if a.providerKeys == nil {
		return nil
	}
	if a.providerKeys.Configured(ctx, p) {
		return nil
	}
	label, ok := providerLabels[p]
	if !ok {
		return fmt.Errorf("model %q requested but its provider is not configured", modelName)
	}
	return fmt.Errorf(
		"%s model %q requested but no %s API key is configured — add one under Integrations → API Keys, or set %s on the server",
		label.name, modelName, label.name, label.envVar,
	)
}
