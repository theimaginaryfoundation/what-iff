package agent

// A utility call (provider.UtilityRequest) runs in three steps:
//
//  1. Resolution (utility_target.go): the call's role picks its model. The account's choice
//     comes first, where accounts bring their own keys; then the deployment's; then the
//     role's built-in model. A choice that can't be used falls back to the next, logged,
//     counted and reported to the chat's background notices. Checkpoint steps whose model
//     follows the chat pick theirs in archival_models.go instead.
//  2. Dispatch (here): the request is validated, then rendered for the API the model speaks
//     (Anthropic Messages, Chat Completions, or OpenAI's Responses) and sent through the
//     same provider clients a chat turn on that model uses.
//  3. Errors (utility_errors.go): a failure names the provider and model it went to, so a
//     background notice can say what failed and the account can act on it.

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

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

// callUtility is CallUtility returning the whole reply. The request is checked before its
// model is resolved, so a malformed one reads no settings and records no fallback.
func (a *Agent) callUtility(ctx context.Context, req provider.UtilityRequest) (utilityReply, error) {
	if err := req.Validate(); err != nil {
		return utilityReply{}, err
	}
	return a.callUtilityTarget(ctx, a.utilityTargetFor(ctx, req.Role), req)
}

// callUtilityTarget makes a utility call on a given model rather than its role's, for the
// calls whose model depends on the chat.
func (a *Agent) callUtilityTarget(ctx context.Context, t utilityTarget, req provider.UtilityRequest) (utilityReply, error) {
	if err := req.Validate(); err != nil {
		return utilityReply{}, err
	}
	if req.CallPath != "" {
		ctx = telemetry.WithCallPath(ctx, req.CallPath)
	}
	return a.callUtilityOn(ctx, t, req)
}

// callUtilityOn sends a utility call to a model through the API it speaks.
func (a *Agent) callUtilityOn(ctx context.Context, t utilityTarget, req provider.UtilityRequest) (utilityReply, error) {
	// A built-in model named without its provider gets it from the catalog here, once, so
	// every helper below routes on the same explicit provider.
	if t.connection == nil && t.provider == "" {
		t.provider = string(models.ProviderForModel("", t.name))
	}
	if req.PreviousResponseID != "" && !t.usesResponsesAPI() {
		return utilityReply{}, fmt.Errorf("model %q cannot continue a stored Responses conversation", t.name)
	}
	lite := t.chatContext()
	// A connection's flavor decides its API before anything about the model's name: a
	// connection model is routed only by its connection, a built-in one only by its provider.
	switch {
	case t.connection != nil && t.connection.Flavor == ConnectionFlavorAnthropic,
		t.connection == nil && models.UsesAnthropicMessagesAPI(t.provider, t.name):
		prov, _, err := a.claudeProviderForModel(lite)
		if err != nil {
			return utilityReply{}, t.missingProvider(err)
		}
		params := provider.UtilityClaudeParams(t.name, req)
		if t.connection == nil && models.IsZAIModel(t.provider, t.name) {
			// GLM always reasons; left alone it can spend the whole cap and answer nothing.
			// Background work wants little of it, as a chat turn with a low effort does.
			provider.ApplyZAIReasoningEffort(&params, anthropic.OutputConfigEffortLow)
		}
		msg, err := prov.Call(ctx, params)
		if err != nil {
			return utilityReply{}, t.callError(err)
		}
		return utilityReply{Text: provider.ExtractClaudeText(msg)}, nil
	case t.connection != nil, models.UsesOpenAIChatCompletionsAPI(t.provider, t.name):
		text, err := a.callUtilityChatCompletions(ctx, t, req)
		if err != nil {
			return utilityReply{}, err
		}
		return utilityReply{Text: text}, nil
	case t.usesResponsesAPI():
		if a.OpenAIProvider == nil {
			return utilityReply{}, t.missingProvider(fmt.Errorf("OpenAI model %q requested but no OpenAI provider is configured", t.name))
		}
		a.recordDroppedImages(ctx, t.name, req.Context)
		resp, err := a.OpenAIProvider.CallWithRetry(ctx, provider.UtilityResponsesParams(t.name, req))
		if err != nil {
			return utilityReply{}, t.callError(err)
		}
		if resp == nil {
			return utilityReply{}, errors.New("utility call returned no response")
		}
		return utilityReply{Text: resp.OutputText(), ResponseID: resp.ID}, nil
	default:
		return utilityReply{}, fmt.Errorf("model %q has no API for utility calls", t.name)
	}
}

// callUtilityChatCompletions sends a utility call through the same Chat
// Completions adapter a chat turn on the model would use, without tools.
func (a *Agent) callUtilityChatCompletions(ctx context.Context, t utilityTarget, req provider.UtilityRequest) (string, error) {
	lite := t.chatContext()
	params := provider.UtilityChatCompletionParams(t.name, req)
	var adapter provider.AgentAdapter
	if t.connection == nil && models.IsGeminiModel(t.provider, t.name) {
		if a.GeminiProvider == nil {
			return "", t.missingProvider(fmt.Errorf("Gemini model %q requested but GEMINI_API_KEY is not configured", t.name))
		}
		adapter = provider.NewGeminiAdapter(a.GeminiProvider, params, nil, nil, a.logger)
	} else {
		var err error
		adapter, err = a.openAIChatCompletionsAdapter(lite, params, nil, nil)
		if err != nil {
			return "", t.missingProvider(err)
		}
	}
	// The adapter offers no tools, so it has no tool uses to return.
	resp, _, err := adapter.Call(ctx)
	if err != nil {
		return "", t.callError(err)
	}
	if resp == nil {
		return "", errors.New("utility call returned no response")
	}
	return resp.Text, nil
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
