package agent

import "github.com/theimaginaryfoundation/what-iff/internal/models"

// callError names the provider a failed utility call went to, since a role's model, and so
// its provider, is not fixed at the call site. A connection model is named by its connection.
func (t utilityTarget) callError(err error) error {
	provider := models.ProviderForModel(t.provider, t.name).DisplayName()
	if t.connection != nil {
		provider = t.connection.Preset
	}
	return &UtilityCallError{Provider: provider, Model: t.name, Err: err}
}

// UtilityCallError is a utility call that failed at, or on the way to, its provider.
type UtilityCallError struct {
	// Provider names the provider the call went to: its display name, or a connection's preset.
	Provider string
	Model    string
	Err      error
}

func (e *UtilityCallError) Error() string { return e.Provider + ": " + e.Err.Error() }

func (e *UtilityCallError) Unwrap() error { return e.Err }
