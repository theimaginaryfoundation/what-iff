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
