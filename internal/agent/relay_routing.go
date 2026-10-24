package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Discounted inference routing (ADR 0x029). Opted-in accounts have chat turns on
// routed models served through the Cheaper Inference relay, with the model's own
// provider as the fallback. Nothing about the model a user picks changes; the route
// shows only in telemetry and on the turn's metering.Usage.Route.

// buildRelayRoute builds the relay route and hands it to every Chat Completions
// provider. It returns nil, routing nothing, when no relay credential is configured.
// Call it after the providers are constructed.
func (a *Agent) buildRelayRoute(cfg AgentConfig, tel *telemetry.Telemetry, logger *zap.Logger) *provider.RelayRoute {
	if cfg.KeyResolver == nil || !cfg.KeyResolver.Configured(models.ModelProviderCheaperInference) {
		return nil
	}
	route := provider.NewCheaperInferenceRoute(cfg.CheaperInferenceBaseURL, cfg.CheaperInferenceZDR, tel, cfg.HTTPClient)
	if a.GeminiProvider != nil {
		a.GeminiProvider.SetRelayRoute(route)
	}
	if a.MistralProvider != nil {
		a.MistralProvider.SetRelayRoute(route)
	}
	if a.DeepSeekProvider != nil {
		a.DeepSeekProvider.SetRelayRoute(route)
	}
	if a.QwenProvider != nil {
		a.QwenProvider.SetRelayRoute(route)
	}
	if a.XiaomiProvider != nil {
		a.XiaomiProvider.SetRelayRoute(route)
	}
	if cfg.CheaperInferenceZDR {
		logger.Info("discounted inference routing enabled for opted-in accounts")
	} else {
		logger.Warn("discounted inference routing enabled with zero data retention OFF; relay requests may be served through third-party seller accounts")
	}
	return route
}

// relayTurnFor returns routing state for a chat turn that may use the relay, or nil
// when it must go direct: no relay, a model outside the route table, or a user who
// has not opted in. The user is loaded only when the first two allow routing.
func (a *Agent) relayTurnFor(ctx context.Context, userID uuid.UUID, resolved resolvedChatModel) *provider.RelayTurn {
	if a.relayRoute == nil || a.ds == nil || !a.relayRoute.Routes(resolved.name) ||
		!models.UsesOpenAIChatCompletionsAPI(resolved.provider, resolved.name) {
		return nil
	}
	u, err := a.ds.GetUserByID(ctx, userID)
	if err != nil {
		a.logger.Warn("relay opt-in lookup failed; turn goes direct", zap.String("user_id", userID.String()), zap.Error(err))
		return nil
	}
	if !u.InferenceRelayOptIn {
		return nil
	}
	return provider.NewRelayTurn()
}
