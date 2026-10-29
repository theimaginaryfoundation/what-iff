// Package provideraccess reports which model providers the requesting account
// can reach, and whether its keys come from the deployment or from the
// account. It states the fact; what to tell the user about it is the
// frontend's.
package provideraccess

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// Handler serves the provider-access route.
type Handler struct {
	resolver providerkeys.Resolver
	logger   *zap.Logger
}

// NewHandler builds the handler over the resolver the server uses.
func NewHandler(resolver providerkeys.Resolver, logger *zap.Logger) *Handler {
	return &Handler{resolver: resolver, logger: logger}
}

// RegisterRoutes mounts the route on an authenticated router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/provider-access", h.Get).Methods(http.MethodGet)
}

// Get reports where the account's keys come from and which providers it has a
// key for. It asks the same resolver every provider request asks, so the
// answer matches what a request would carry.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	access := models.ProviderAccess{
		KeysFrom:  models.KeysFromDeployment,
		Providers: []models.ModelProvider{},
	}
	if h.resolver.AccountsSupplyKeys() {
		access.KeysFrom = models.KeysFromAccount
	}
	for _, p := range models.ModelProviders {
		if h.resolver.Key(r.Context(), p) != "" {
			access.Providers = append(access.Providers, p)
		}
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, access)
}
