package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
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

// utilityReply is a utility call's reply.
type utilityReply struct {
	Text string
	// ResponseID is the stored response's ID on the Responses API, which a
	// later call can continue from; empty on other APIs.
	ResponseID string
}

// CallUtility makes a utility call on its role's model, through whichever API
// that model speaks, and returns the reply's text.
func (a *Agent) CallUtility(ctx context.Context, req provider.UtilityRequest) (string, error) {
	reply, err := a.callUtility(ctx, req)
	return reply.Text, err
}

// callUtility is CallUtility returning the whole reply.
func (a *Agent) callUtility(ctx context.Context, req provider.UtilityRequest) (utilityReply, error) {
	return a.callUtilityModel(ctx, utilityModelName(req.Role), req)
}

// callUtilityModel makes a utility call on a given model rather than its
// role's, for the calls whose model depends on the chat.
func (a *Agent) callUtilityModel(ctx context.Context, model string, req provider.UtilityRequest) (utilityReply, error) {
	if err := req.Validate(); err != nil {
		return utilityReply{}, err
	}
	if req.CallPath != "" {
		ctx = telemetry.WithCallPath(ctx, req.CallPath)
	}
	return a.callUtilityOn(ctx, model, "", req)
}

// callUtilityOn sends a utility call to a model. modelProvider is the model
// row's provider, or empty to infer it from the name.
func (a *Agent) callUtilityOn(ctx context.Context, model, modelProvider string, req provider.UtilityRequest) (utilityReply, error) {
	responsesAPI := !models.UsesAnthropicMessagesAPI(modelProvider, model) &&
		!models.UsesOpenAIChatCompletionsAPI(modelProvider, model) &&
		models.ProviderForModel(modelProvider, model) == models.ModelProviderOpenAI
	if req.PreviousResponseID != "" && !responsesAPI {
		return utilityReply{}, fmt.Errorf("model %q cannot continue a stored Responses conversation", model)
	}
	switch {
	case models.UsesAnthropicMessagesAPI(modelProvider, model):
		prov, _, err := a.claudeProviderForModel(&chatContext{model: model, modelProvider: modelProvider})
		if err != nil {
			return utilityReply{}, err
		}
		msg, err := prov.Call(ctx, provider.UtilityClaudeParams(model, req))
		if err != nil {
			return utilityReply{}, utilityCallError(modelProvider, model, err)
		}
		return utilityReply{Text: provider.ExtractClaudeText(msg)}, nil
	case models.UsesOpenAIChatCompletionsAPI(modelProvider, model):
		text, err := a.callUtilityChatCompletions(ctx, model, modelProvider, req)
		if err != nil {
			return utilityReply{}, err
		}
		return utilityReply{Text: text}, nil
	case responsesAPI:
		if a.OpenAIProvider == nil {
			return utilityReply{}, fmt.Errorf("OpenAI model %q requested but no OpenAI provider is configured", model)
		}
		a.recordDroppedImages(ctx, model, req.Context)
		resp, err := a.OpenAIProvider.CallWithRetry(ctx, provider.UtilityResponsesParams(model, req))
		if err != nil {
			return utilityReply{}, utilityCallError(modelProvider, model, err)
		}
		if resp == nil {
			return utilityReply{}, errors.New("utility call returned no response")
		}
		return utilityReply{Text: resp.OutputText(), ResponseID: resp.ID}, nil
	default:
		return utilityReply{}, fmt.Errorf("model %q has no API for utility calls", model)
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
	// The adapter offers no tools, so it has no tool uses to return.
	resp, _, err := adapter.Call(ctx)
	if err != nil {
		return "", utilityCallError(modelProvider, model, err)
	}
	if resp == nil {
		return "", errors.New("utility call returned no response")
	}
	return resp.Text, nil
}

// utilityCallError names the provider a failed utility call went to, since a role's model,
// and so its provider, is not fixed at the call site.
func utilityCallError(modelProvider, model string, err error) error {
	return fmt.Errorf("%s: %w", models.ProviderForModel(modelProvider, model).DisplayName(), err)
}

// recordDroppedImages logs and counts the images a conversation loses on its way to the
// Responses API, which can't take them; the call goes ahead on the rest rather than failing
// over one image.
func (a *Agent) recordDroppedImages(ctx context.Context, model string, mc *provider.ModelContext) {
	prov := models.ModelProviderOpenAI
	noData, unsupported := provider.OpenAIRejectedImages(mc)
	if noData+unsupported == 0 {
		return
	}
	callPath := telemetry.CallPathFromContext(ctx)
	a.logger.Warn("utility call left out images the provider can't take",
		zap.String("provider", string(prov)),
		zap.String("model", model),
		zap.String("call_path", string(callPath)),
		zap.Int("no_data", noData),
		zap.Int("unsupported_type", unsupported))
	for reason, n := range map[string]int{"no_data": noData, "unsupported_type": unsupported} {
		a.metrics().Add(ctx, telemetry.GenAIImagesDropped, int64(n),
			telemetry.AttrGenAIProvider.String(string(prov)),
			telemetry.AttrCallPath.String(string(callPath)),
			telemetry.AttrReason.String(reason))
	}
}
