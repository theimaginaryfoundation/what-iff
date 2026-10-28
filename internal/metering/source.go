package metering

import "context"

// TurnSource says where a turn was started from, so an implementation can meter it apart from
// the owner's own chat traffic: a turn a stranger starts through a plugin (a Discord relay, say)
// or a webhook is not the owner typing in the app. The agent carries the source from the request
// into the turn's own context, so Meter.Check reads it with TurnSourceFromContext and Record
// gets it in Usage.Source. An empty source is a turn whose entry point did not say (treat it as
// the app's).
const (
	TurnSourceApp     = "app"
	TurnSourceWebhook = "webhook"
	// TurnSourcePlugin is the default for a plugin-started turn; a plugin names itself instead
	// (plugins.UserTurn.Source), so the relay's turns are "discord", not "plugin".
	TurnSourcePlugin = "plugin"
)

type ctxKeyTurnSource struct{}

// WithTurnSource returns a child context that carries the turn's source.
func WithTurnSource(ctx context.Context, source string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if source == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyTurnSource{}, source)
}

// TurnSourceFromContext returns the turn's source, or "" when none was set.
func TurnSourceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyTurnSource{}).(string)
	return v
}

type ctxKeyAgentLoop struct{}

// WithAgentLoop returns a child context carrying the turn's agent loop ("standard" or
// "advanced"; ADR 0x026), so Meter.Check can gate an Advanced turn differently before it runs.
func WithAgentLoop(ctx context.Context, loop string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if loop == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyAgentLoop{}, loop)
}

// AgentLoopFromContext returns the turn's agent loop, or "" (standard) when none was set.
func AgentLoopFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyAgentLoop{}).(string)
	return v
}

type ctxKeyPendingUsage struct{}

// WithPendingUsage returns a child context carrying usage a running turn has incurred but not yet
// recorded: the calls an Advanced step has made so far (ADR 0x026). Check is asked with it before
// each further model call, so a meter can refuse once recording what is pending would leave
// nothing to pay for another call, instead of only seeing the balance from before the step.
func WithPendingUsage(ctx context.Context, u Usage) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKeyPendingUsage{}, u)
}

// PendingUsageFromContext returns the usage WithPendingUsage attached, if any.
func PendingUsageFromContext(ctx context.Context) (Usage, bool) {
	if ctx == nil {
		return Usage{}, false
	}
	u, ok := ctx.Value(ctxKeyPendingUsage{}).(Usage)
	return u, ok
}
