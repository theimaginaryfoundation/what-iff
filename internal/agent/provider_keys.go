package agent

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// providerLabels names a provider for humans, plus the environment variable an
// operator would set.
//
// The two are used in different places on purpose. The label goes in the error
// returned to the caller, which states a fact about their account. The
// environment variable goes only to the server log, where the operator is: an
// end user cannot act on it, and on a hosted deployment they are not the person
// who would.
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
		return fmt.Errorf("model %q is unavailable: its provider is not configured", modelName)
	}
	// Operator-facing remediation goes to the log, not to the caller. Which
	// remedy is even correct differs by deployment — supplying your own key is
	// a self-hosted affordance, not a hosted one — so naming one in a shared
	// error would be wrong for half of them. What to do about it is the
	// interface's job; this states what is true.
	if a.logger != nil {
		a.logger.Warn("model request blocked: provider credential missing",
			zap.String("provider", string(p)),
			zap.String("model", modelName),
			zap.String("deployment_env_var", label.envVar))
	}
	return fmt.Errorf("%s model %q is unavailable: no %s API key is configured for this account",
		label.name, modelName, label.name)
}
