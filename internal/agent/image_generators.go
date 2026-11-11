package agent

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// buildImageGenerators returns an image generator for every vendor this
// deployment has a credential for; OpenAI's wraps the agent's OpenAIProvider.
// Like the chat providers, each is built without a key and credentialed per
// request by cfg.HTTPClient.
func buildImageGenerators(cfg AgentConfig, a *Agent) map[models.ModelProvider]provider.ImageGenerator {
	gens := make(map[models.ModelProvider]provider.ImageGenerator, len(provider.ImageGenerationProviders))
	configured := func(p models.ModelProvider) bool {
		return cfg.KeyResolver != nil && cfg.KeyResolver.Configured(p)
	}
	if a.OpenAIProvider != nil && configured(models.ModelProviderOpenAI) {
		gens[models.ModelProviderOpenAI] = a.OpenAIProvider.ImageGenerator()
	}
	if configured(models.ModelProviderGoogle) {
		gens[models.ModelProviderGoogle] = provider.NewGeminiImageGenerator(cfg.HTTPClient, provider.GeminiNativeBaseURL(cfg.GeminiBaseURL), a.telemetry)
	}
	if configured(models.ModelProviderZAI) {
		gens[models.ModelProviderZAI] = provider.NewZAIImageGenerator(cfg.HTTPClient, provider.ZAIPaaSBaseURL(cfg.ZAIBaseURL), a.telemetry)
	}
	if configured(models.ModelProviderQwen) {
		gens[models.ModelProviderQwen] = provider.NewQwenImageGenerator(cfg.HTTPClient, provider.QwenNativeBaseURL(cfg.QwenBaseURL), a.telemetry)
	}
	return gens
}

// imageGeneratorForUser returns the image generator for the vendor the user
// picked (UserPreference image_generation_provider).
//
// A pick the caller holds no key for (removed since, or never added) falls back
// to the first usable vendor, OpenAI when it is, rather than failing the user's
// request; the fallback is logged. An Agent built without NewAgent has no
// registry and always uses OpenAIProvider.
func (a *Agent) imageGeneratorForUser(ctx context.Context, userID uuid.UUID) (provider.ImageGenerator, error) {
	usable := a.usableImageGenerators(ctx)
	want := models.DefaultImageGenerationProvider
	// With one vendor usable there is nothing to choose, so skip the read.
	if len(usable) > 1 {
		chosen, err := a.imageGenerationPreference(ctx, userID)
		if err != nil {
			a.logger.Warn("could not read image generation preference; using the default vendor",
				zap.String("user_id", userID.String()), zap.Error(err))
		} else if chosen != "" {
			want = chosen
		}
	}
	for _, gen := range usable {
		if gen.Provider() == want {
			return gen, nil
		}
	}
	// Fall back in the settings UI's order, OpenAI first, so a caller with only
	// (say) a Gemini key still gets images, chosen or not.
	if len(usable) > 0 {
		if want != models.DefaultImageGenerationProvider {
			a.logger.Warn("chosen image generation vendor has no key for this caller; using another",
				zap.String("user_id", userID.String()), zap.String("provider", string(want)), zap.String("using", string(usable[0].Provider())))
		}
		return usable[0], nil
	}
	if a.imageGenerators == nil && a.OpenAIProvider != nil {
		return a.OpenAIProvider.ImageGenerator(), nil
	}
	return nil, errNoImageGenerator
}

// errNoImageGenerator means no image vendor is usable at all: an operational failure, not a refusal.
var errNoImageGenerator = errors.New("no image generation provider is available")

// usableImageGenerators lists, in the settings UI's order, the generators this
// caller holds a key for. Keys can belong to accounts (providerkeys), so this
// is asked per request rather than settled when the registry is built.
func (a *Agent) usableImageGenerators(ctx context.Context) []provider.ImageGenerator {
	var usable []provider.ImageGenerator
	for _, p := range provider.ImageGenerationProviders {
		gen, ok := a.imageGenerators[p]
		if !ok {
			continue
		}
		if a.keyResolver != nil && a.keyResolver.Key(ctx, p) == "" {
			continue
		}
		usable = append(usable, gen)
	}
	return usable
}

// imageGenerationPreference reads the user's image_generation_provider.
func (a *Agent) imageGenerationPreference(ctx context.Context, userID uuid.UUID) (models.ModelProvider, error) {
	if a.testHooks.ImageGenerationPreference != nil {
		return a.testHooks.ImageGenerationPreference(ctx, userID)
	}
	if a.ds == nil {
		return "", nil
	}
	prefs, err := a.ds.GetUserPreferences(ctx, userID)
	if err != nil || prefs == nil {
		return "", err
	}
	return models.ModelProvider(prefs.ImageGenerationProvider), nil
}

// imageSafetyProvider maps an image vendor to the provider recorded on a
// safety violation event.
func imageSafetyProvider(p models.ModelProvider) models.SafetyViolationProvider {
	switch p {
	case models.ModelProviderGoogle:
		return models.SafetyViolationProviderGoogle
	case models.ModelProviderZAI:
		return models.SafetyViolationProviderZAI
	case models.ModelProviderQwen:
		return models.SafetyViolationProviderQwen
	default:
		return models.SafetyViolationProviderOpenAI
	}
}
