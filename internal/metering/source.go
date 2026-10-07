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
