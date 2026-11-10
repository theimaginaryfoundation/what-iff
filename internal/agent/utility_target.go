package agent

// Which model a utility call runs on. utility_call.go describes the whole flow.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// ErrUtilityRoleOff is a utility call whose role is switched off: its work doesn't run.
var ErrUtilityRoleOff = errors.New("this utility role is switched off")

// ErrUtilityRoleNotSet is a utility call whose role has no model chosen at any scope: its work
// doesn't run until one is.
var ErrUtilityRoleNotSet = errors.New("no model is chosen for this utility role")

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

// resolveUtilityTarget is the model chosen for role by the account the context is for, or else
// by the deployment (ADR 0x027 section 6).
//
// A choice that can't be used (turned off, deleted with its connection, its connection
// unreachable, its key gone) falls back to the next scope's, logged and counted; past the last,
// the call fails with why the last one couldn't be used. A role switched off at the first scope
// that chose for it is ErrUtilityRoleOff, and one no scope chose is ErrUtilityRoleNotSet. A
// failed read of the settings fails the call.
func (a *Agent) resolveUtilityTarget(ctx context.Context, role provider.UtilityRole) (utilityTarget, error) {
	if a.ds == nil {
		return utilityTarget{}, ErrUtilityRoleNotSet
	}
	userID, _ := middleware.GetUserIDFromContext(ctx)
	settings, err := a.ds.ListUtilityModelSettings(ctx, userID)
	if err != nil {
		a.recordUtilityFallback(ctx, role, "", uuid.Nil, err)
		return utilityTarget{}, fmt.Errorf("read utility model settings: %w", err)
	}
	var unusable error
	for _, scope := range a.utilityScopes() {
		for _, s := range settings {
			if s.Role != role || s.Scope != scope {
				continue
			}
			if s.Off() {
				return utilityTarget{}, ErrUtilityRoleOff
			}
			t, err := a.settingTarget(ctx, userID, s)
			if err == nil {
				return t, nil
			}
			a.recordUtilityFallback(ctx, role, scope, *s.ModelID, err)
			unusable = err
		}
	}
	if unusable != nil {
		return utilityTarget{}, unusable
	}
	return utilityTarget{}, ErrUtilityRoleNotSet
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
	m, err := a.ds.GetModel(ctx, *s.ModelID)
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
	if conn == nil {
		if err := a.catalogChoiceUsable(ctx, userID, s.Scope, m); err != nil {
			return utilityTarget{}, err
		}
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
	a.logger.Warn("utility role is passing over a choice it can't use", append(fields,
		zap.String("reason", reason),
		zap.Error(err))...)
	a.metrics().Add(ctx, telemetry.UtilityModelFallbacks, 1,
		telemetry.AttrUtilityRole.String(string(role)),
		telemetry.AttrUtilityScope.String(string(scope)),
		telemetry.AttrReason.String(reason))
}

// utilityTargetFor is the model a role's calls run on: the one pinned on the context, or else
// the chosen one (resolveUtilityTarget).
func (a *Agent) utilityTargetFor(ctx context.Context, role provider.UtilityRole) (utilityTarget, error) {
	if t, ok := pinnedUtilityTarget(ctx, role); ok {
		return t, nil
	}
	return a.resolveUtilityTarget(ctx, role)
}

// catalogChoiceUsable checks what a catalog model's choice was saved with and can lose since:
// a key for its provider, and, for an account's choice of an experimental model, the account's
// opt-in. Either lost makes the choice unusable, so the role falls back rather than failing at
// every call.
func (a *Agent) catalogChoiceUsable(ctx context.Context, userID uuid.UUID, scope models.UtilityScope, m *models.Model) error {
	p := models.ProviderForModel(m.Provider, m.Name)
	if a.keyResolver != nil && a.keyResolver.Key(ctx, p) == "" {
		return &providerkeys.NotConfiguredError{Provider: p}
	}
	if scope != models.UtilityScopeAccount || !models.IsExperimentalModel(m) {
		return nil
	}
	u, err := a.ds.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !u.EnableExperimentalModels {
		return datastore.ErrExperimentalModelNotAllowed
	}
	return nil
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
