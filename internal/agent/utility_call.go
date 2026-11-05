package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// utilityBuiltIn is the model a utility role runs on, and its provider: the fixed model each
// role's call sites used before roles existed. It is the one place a role's model is decided;
// roles become settings in a later step (ADR 0x027 section 6). The provider is named rather
// than looked up from the catalog, so a call routes even if the catalog drops the model.
func utilityBuiltIn(role provider.UtilityRole) (model string, modelProvider models.ModelProvider) {
	switch role {
	case provider.UtilityRoleMemory:
		return archivalOpenAIModel, models.ModelProviderOpenAI
	case provider.UtilityRolePersonality:
		return generatePersonalityModel, models.ModelProviderOpenAI
	default:
		return models.UtilityModelName, models.ModelProviderOpenAI
	}
}

// UtilityBuiltInModel names the model a utility role runs on when neither the account nor the
// deployment chose one.
func UtilityBuiltInModel(role models.UtilityRole) string {
	name, _ := utilityBuiltIn(role)
	return name
}

// utilityTarget is the model a utility call runs on.
type utilityTarget struct {
	name string
	// provider is the model row's provider, the connection's preset for a connection model,
	// or empty to infer it from the name.
	provider string
	// connection reaches a model from an OpenRouter, local or Custom connection; nil for a
	// model on a built-in provider's own path.
	connection *ConnectionEndpoint
}

// usesResponsesAPI reports whether the target is on OpenAI's own Responses API, the only one
// that can continue a stored conversation.
func (t utilityTarget) usesResponsesAPI() bool {
	return t.connection == nil &&
		!models.UsesAnthropicMessagesAPI(t.provider, t.name) &&
		!models.UsesOpenAIChatCompletionsAPI(t.provider, t.name) &&
		models.ProviderForModel(t.provider, t.name) == models.ModelProviderOpenAI
}

// chosenUtilityTarget is the model chosen for role by the account the context is for, or
// else by the deployment (ADR 0x027 section 6), and whether either chose one that can be used.
//
// A choice that can't be used (turned off, deleted with its connection, its connection
// unreachable) falls back to the next one, and past the last to the role's built-in model, so
// background work keeps running; each fallback is logged and counted, so it shows. A failed
// read of the settings falls back the same way.
func (a *Agent) chosenUtilityTarget(ctx context.Context, role provider.UtilityRole) (utilityTarget, bool) {
	if a.ds == nil {
		return utilityTarget{}, false
	}
	userID, _ := middleware.GetUserIDFromContext(ctx)
	settings, err := a.ds.ListUtilityModelSettings(ctx, userID)
	if err != nil {
		a.recordUtilityFallback(ctx, role, "", uuid.Nil, err)
		return utilityTarget{}, false
	}
	for _, scope := range a.utilityScopes() {
		for _, s := range settings {
			if s.Role != role || s.Scope != scope {
				continue
			}
			t, err := a.settingTarget(ctx, userID, s)
			if err == nil {
				return t, true
			}
			a.recordUtilityFallback(ctx, role, scope, s.ModelID, err)
		}
	}
	return utilityTarget{}, false
}

// utilityScopes are the scopes whose choices count, first wins. An account chooses only where it
// brings its own keys and so pays for its background work; where the deployment supplies the
// keys, it pays, and only its choices count, whatever an account stored before.
func (a *Agent) utilityScopes() []models.UtilityScope {
	if a.keyResolver != nil && !a.keyResolver.AccountsSupplyKeys() {
		return []models.UtilityScope{models.UtilityScopeDeployment}
	}
	return []models.UtilityScope{models.UtilityScopeAccount, models.UtilityScopeDeployment}
}

// settingTarget is the model a setting chose, if it can still be used.
func (a *Agent) settingTarget(ctx context.Context, userID uuid.UUID, s models.UtilityModelSetting) (utilityTarget, error) {
	m, err := a.ds.GetModel(ctx, s.ModelID)
	if errors.Is(err, datastore.ErrModelNotFound) {
		return utilityTarget{}, ErrModelDisabled
	}
	if err != nil {
		return utilityTarget{}, err
	}
	conn, err := a.connectionFor(ctx, userID, m)
	if err != nil {
		return utilityTarget{}, err
	}
	t := utilityTarget{name: m.Name, provider: m.Provider, connection: conn}
	if conn != nil {
		t.provider = conn.Preset
	}
	return t, nil
}

// recordUtilityFallback logs and counts a role passing over a choice it couldn't use, or
// settings it couldn't read (an empty scope).
func (a *Agent) recordUtilityFallback(ctx context.Context, role provider.UtilityRole, scope models.UtilityScope, modelID uuid.UUID, err error) {
	reason := "unreadable"
	if scope != "" {
		reason = "unavailable"
	}
	a.logger.Warn("utility role is falling back past its chosen model",
		zap.String("role", string(role)),
		zap.String("scope", string(scope)),
		zap.String("model_id", modelID.String()),
		zap.String("reason", reason),
		zap.Error(err))
	a.metrics().Add(ctx, telemetry.UtilityModelFallbacks, 1,
		telemetry.AttrUtilityRole.String(string(role)),
		telemetry.AttrUtilityScope.String(string(scope)),
		telemetry.AttrReason.String(reason))
}

// utilityTargetFor is the model a role's calls run on: the chosen one, or its built-in model.
func (a *Agent) utilityTargetFor(ctx context.Context, role provider.UtilityRole) utilityTarget {
	if t, chosen := a.chosenUtilityTarget(ctx, role); chosen {
		return t
	}
	name, modelProvider := utilityBuiltIn(role)
	return utilityTarget{name: name, provider: string(modelProvider)}
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
	lite := &chatContext{model: t.name, modelProvider: t.provider, connection: t.connection}
	switch {
	case t.connection != nil && t.connection.Flavor == ConnectionFlavorAnthropic,
		t.connection == nil && models.UsesAnthropicMessagesAPI(t.provider, t.name):
		prov, _, err := a.claudeProviderForModel(lite)
		if err != nil {
			return utilityReply{}, err
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
		text, err := a.callUtilityChatCompletions(ctx, t, lite, req)
		if err != nil {
			return utilityReply{}, err
		}
		return utilityReply{Text: text}, nil
	case t.usesResponsesAPI():
		if a.OpenAIProvider == nil {
			return utilityReply{}, fmt.Errorf("OpenAI model %q requested but no OpenAI provider is configured", t.name)
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
func (a *Agent) callUtilityChatCompletions(ctx context.Context, t utilityTarget, lite *chatContext, req provider.UtilityRequest) (string, error) {
	params := provider.UtilityChatCompletionParams(t.name, req)
	var adapter provider.AgentAdapter
	if t.connection == nil && models.IsGeminiModel(t.provider, t.name) {
		if a.GeminiProvider == nil {
			return "", fmt.Errorf("Gemini model %q requested but GEMINI_API_KEY is not configured", t.name)
		}
		adapter = provider.NewGeminiAdapter(a.GeminiProvider, params, nil, nil, a.logger)
	} else {
		var err error
		adapter, err = a.openAIChatCompletionsAdapter(lite, params, nil, nil)
		if err != nil {
			return "", err
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

// callError names the provider a failed utility call went to, since a role's model, and so
// its provider, is not fixed at the call site. A connection model is named by its connection.
func (t utilityTarget) callError(err error) error {
	if t.connection != nil {
		return fmt.Errorf("%s: %w", t.connection.Preset, err)
	}
	return fmt.Errorf("%s: %w", models.ProviderForModel(t.provider, t.name).DisplayName(), err)
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
