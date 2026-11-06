package agent

// Which model a utility call runs on. utility_call.go describes the whole flow.

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// utilityBuiltIn is the model a utility role runs on when neither the account nor the
// deployment chose one, and its provider: the fixed model each role's call sites used before
// roles existed. The provider is named rather than looked up from the catalog, so a call
// routes even if the catalog drops the model.
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
// deployment chose one. GET /utility-models reports it as built_in_model, and openapi.yaml
// describes that field.
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

// chatContext is the part of a chat context the provider helpers route on, for a call that
// has no chat of its own.
func (t utilityTarget) chatContext() *chatContext {
	return &chatContext{model: t.name, modelProvider: t.provider, connection: t.connection}
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
	fields := []zap.Field{zap.String("role", string(role)), zap.String("scope", string(scope))}
	if modelID != uuid.Nil {
		fields = append(fields, zap.String("model_id", modelID.String()))
	}
	a.logger.Warn("utility role is falling back past its chosen model", append(fields,
		zap.String("reason", reason),
		zap.Error(err))...)
	a.metrics().Add(ctx, telemetry.UtilityModelFallbacks, 1,
		telemetry.AttrUtilityRole.String(string(role)),
		telemetry.AttrUtilityScope.String(string(scope)),
		telemetry.AttrReason.String(reason))
}

// utilityTargetFor is the model a role's calls run on: the chosen one, or its built-in model.
func (a *Agent) utilityTargetFor(ctx context.Context, role provider.UtilityRole) utilityTarget {
	if t, ok := pinnedUtilityTarget(ctx, role); ok {
		return t
	}
	if t, chosen := a.chosenUtilityTarget(ctx, role); chosen {
		return t
	}
	name, modelProvider := utilityBuiltIn(role)
	return utilityTarget{name: name, provider: string(modelProvider)}
}

// pinnedUtilityTargetKey carries a role's target, already resolved, on a context.
type pinnedUtilityTargetKey struct{ role provider.UtilityRole }

// withUtilityTarget pins role's calls under ctx to t, so they all run on one model however the
// settings change meanwhile. A checkpoint pins its memory model: its steps continue one stored
// response, which only the model that decided to chain off it can do.
func withUtilityTarget(ctx context.Context, role provider.UtilityRole, t utilityTarget) context.Context {
	return context.WithValue(ctx, pinnedUtilityTargetKey{role}, t)
}

// pinnedUtilityTarget is the target withUtilityTarget pinned for role on ctx, if any.
func pinnedUtilityTarget(ctx context.Context, role provider.UtilityRole) (utilityTarget, bool) {
	t, ok := ctx.Value(pinnedUtilityTargetKey{role}).(utilityTarget)
	return t, ok
}
