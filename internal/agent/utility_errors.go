package agent

import (
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// callError names the provider a failed utility call went to, since a role's model, and so
// its provider, is not fixed at the call site. A connection model is named by its connection.
func (t utilityTarget) callError(err error) error {
	provider := models.ProviderForModel(t.provider, t.name).DisplayName()
	if t.connection != nil {
		provider = connectionPresetName(t.connection.Preset)
	}
	return &UtilityCallError{Provider: provider, Model: t.name, Err: err}
}

// missingProvider marks a utility call that never reached a provider because this deployment
// has no client for it; the account can only fix it by choosing another model.
func (t utilityTarget) missingProvider(err error) error {
	return t.callError(fmt.Errorf("%w: %w", errUtilityProviderMissing, err))
}

// clientError reports a failure to pick the client for a utility call: the provider not set up
// on this deployment, which the account can act on, or anything else, a routing bug, which
// stays a plain error for the logs rather than becoming a notice.
func (t utilityTarget) clientError(err error) error {
	var notConfigured *providerNotConfiguredError
	if errors.As(err, &notConfigured) {
		return t.missingProvider(err)
	}
	return fmt.Errorf("utility call on %q: %w", t.name, err)
}

// errUtilityProviderMissing is a utility call's provider not being set up on this deployment.
var errUtilityProviderMissing = errors.New("provider not set up")

// connectionPresetName is how a person knows a connection preset.
func connectionPresetName(preset string) string {
	switch preset {
	case "openrouter":
		return "OpenRouter"
	case "ollama":
		return "Ollama"
	case "lmstudio":
		return "LM Studio"
	case "custom":
		return "your custom server"
	}
	return preset
}

// UtilityCallError is a utility call that failed at, or on the way to, its provider.
type UtilityCallError struct {
	// Provider names the provider the call went to: its display name, or a connection's preset.
	Provider string
	Model    string
	Err      error
}

func (e *UtilityCallError) Error() string {
	if e.Model == "" {
		return e.Provider + ": " + e.Err.Error()
	}
	return e.Provider + " (" + e.Model + "): " + e.Err.Error()
}

func (e *UtilityCallError) Unwrap() error { return e.Err }
