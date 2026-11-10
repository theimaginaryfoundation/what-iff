package handlerutils

import (
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// RespondIfProviderNotConfigured answers a request that failed because the
// account has no key for a provider it needed, and reports whether it did.
// The response is 409 with code provider_not_configured and details
// {"provider": "<name>"}: the request was valid, the account's setup could
// not serve it, and the same request succeeds once the key is added.
func RespondIfProviderNotConfigured(w http.ResponseWriter, logger *zap.Logger, err error) bool {
	var notConfigured *providerkeys.NotConfiguredError
	if !errors.As(err, &notConfigured) {
		return false
	}
	RespondWithErrorDetails(w, logger, http.StatusConflict, models.ErrCodeProviderNotConfigured,
		notConfigured.Message(), map[string]models.ModelProvider{"provider": notConfigured.Provider}, nil)
	return true
}

// RespondIfUtilityRoleUnavailable answers a request that needed background work its utility
// role's choice keeps from running, and reports whether it did. The response is 409 with code
// utility_model_not_set (nothing chosen) or utility_role_off (switched off), and details
// {"role": "<role>"}: the request was valid, and succeeds once the role has a model.
func RespondIfUtilityRoleUnavailable(w http.ResponseWriter, logger *zap.Logger, err error) bool {
	var unavailable *models.UtilityRoleUnavailableError
	if !errors.As(err, &unavailable) {
		return false
	}
	code, message := models.ErrCodeUtilityModelNotSet, "No model is chosen for this background work"
	if errors.Is(err, models.ErrUtilityRoleOff) {
		code, message = models.ErrCodeUtilityRoleOff, "This background work is switched off"
	}
	RespondWithErrorDetails(w, logger, http.StatusConflict, code, message,
		map[string]models.UtilityRole{"role": unavailable.Role}, nil)
	return true
}
