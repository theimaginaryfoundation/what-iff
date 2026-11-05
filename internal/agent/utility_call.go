package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// utilityModelName is the model a utility role runs on. These are the fixed
// choices each call site made before roles existed; roles become settings in a
// later step (ADR 0x027 section 6).
func utilityModelName(role provider.UtilityRole) string {
	switch role {
	case provider.UtilityRoleMemory:
		return archivalOpenAIModel
	case provider.UtilityRolePersonality:
		return generatePersonalityModel
	default:
		return models.UtilityModelName
	}
}

// CallUtility makes a utility call on its role's model, through whichever API
// that model speaks, and returns the reply's text.
func (a *Agent) CallUtility(ctx context.Context, req provider.UtilityRequest) (string, error) {
	if req.CallPath != "" {
		ctx = telemetry.WithCallPath(ctx, req.CallPath)
	}
	return a.callUtilityOn(ctx, utilityModelName(req.Role), "", req)
}

// callUtilityOn makes a utility call on a given model. modelProvider is the
// model row's provider, or empty to infer it from the name.
func (a *Agent) callUtilityOn(ctx context.Context, model, modelProvider string, req provider.UtilityRequest) (string, error) {
	switch {
	case models.UsesAnthropicMessagesAPI(modelProvider, model):
		prov, _, err := a.claudeProviderForModel(&chatContext{model: model, modelProvider: modelProvider})
		if err != nil {
			return "", err
		}
		msg, err := prov.Call(ctx, provider.UtilityClaudeParams(model, req))
		if err != nil {
			return "", err
		}
		return provider.ExtractClaudeText(msg), nil
	case models.UsesOpenAIChatCompletionsAPI(modelProvider, model):
		return a.callUtilityChatCompletions(ctx, model, modelProvider, req)
	case models.ProviderForModel(modelProvider, model) == models.ModelProviderOpenAI:
		if a.OpenAIProvider == nil {
			return "", fmt.Errorf("OpenAI model %q requested but no OpenAI provider is configured", model)
		}
		resp, err := a.OpenAIProvider.CallWithRetry(ctx, provider.UtilityResponsesParams(model, req))
		if err != nil {
			return "", err
		}
		if resp == nil {
			return "", errors.New("utility call returned no response")
		}
		return resp.OutputText(), nil
	default:
		return "", fmt.Errorf("model %q has no API for utility calls", model)
	}
}

// callUtilityChatCompletions sends a utility call through the same Chat
// Completions adapter a chat turn on the model would use, without tools.
func (a *Agent) callUtilityChatCompletions(ctx context.Context, model, modelProvider string, req provider.UtilityRequest) (string, error) {
	params := provider.UtilityChatCompletionParams(model, req)
	var adapter provider.AgentAdapter
	if models.IsGeminiModel(modelProvider, model) {
		if a.GeminiProvider == nil {
			return "", fmt.Errorf("Gemini model %q requested but GEMINI_API_KEY is not configured", model)
		}
		adapter = provider.NewGeminiAdapter(a.GeminiProvider, params, nil, nil, a.logger)
	} else {
		var err error
		adapter, err = a.openAIChatCompletionsAdapter(&chatContext{model: model, modelProvider: modelProvider}, params, nil, nil)
		if err != nil {
			return "", err
		}
	}
	resp, _, err := adapter.Call(ctx)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("utility call returned no response")
	}
	return resp.Text, nil
}
