// Package providerkey exposes an account's own model-provider API keys.
//
// Every route here is scoped to the authenticated caller: the account is taken
// from the request context, never from the payload, so one user cannot read or
// replace another's credential by guessing an id. There are no ids in these
// routes at all — an account has at most one key per provider.
package providerkey

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/providermodels"
	"github.com/theimaginaryfoundation/what-iff/internal/providerpolicy"
)

// Handler serves the per-account provider-key routes.
type Handler struct {
	ds        *datastore.Datastore
	logger    *zap.Logger
	resolvers map[string]*providerkeys.Resolver
	// providerModels is optional; a build without it serves 503 on the listing
	// route rather than failing to start.
	providerModels *providermodels.Service
}

// NewHandler builds the handler. resolvers are keyed by provider and are
// notified on write; a provider with no resolver still stores fine, it just
// has no cache to clear.
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, resolvers map[string]*providerkeys.Resolver, providerModels *providermodels.Service) *Handler {
	return &Handler{ds: ds, logger: logger, resolvers: resolvers, providerModels: providerModels}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	r := router.PathPrefix("/provider-keys").Subrouter()
	r.HandleFunc("", h.ListKeys).Methods("GET")
	r.HandleFunc("/{provider}", h.SetKey).Methods("PUT")
	r.HandleFunc("/{provider}", h.DeleteKey).Methods("DELETE")

	// Deliberately its own prefix rather than /provider-keys/usage, which the
	// {provider} route would otherwise swallow.
	router.HandleFunc("/provider-usage", h.ListUsage).Methods("GET")
	router.HandleFunc("/provider-models/{provider}", h.ListProviderModels).Methods("GET")
}

// ListProviderModels asks the provider which models it serves, using this
// account's key.
//
// Nothing is filtered away here. Retired and non-chat models are returned with
// their kind and shutdown date so the caller can decide — the classification is
// a heuristic over model ids, and hiding a model the heuristic misjudged would
// make it permanently unreachable rather than merely mislabelled.
func (h *Handler) ListProviderModels(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if h.providerModels == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "Model listing is not configured", nil)
		return
	}

	providerName := strings.ToLower(strings.TrimSpace(mux.Vars(r)["provider"]))
	refresh := strings.EqualFold(r.URL.Query().Get("refresh"), "true")

	// Listing a provider's catalog spends a credential and reveals what that
	// credential can reach. Where the operator supplies the keys, neither is the
	// account's to direct — it would be one user spending the operator's key and
	// reading back the operator's model access.
	if !providerpolicy.AccountsSupplyKeys() {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet,
			"Listing a provider's models is not available on this deployment", nil)
		return
	}

	list, err := h.providerModels.List(r.Context(), userID, providerName, refresh)
	if err != nil {
		if errors.Is(err, providermodels.ErrUnsupportedProvider) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet,
				"This build cannot list models for that provider yet", nil)
			return
		}
		// The provider's own error can echo the key back, so the caller gets a
		// generic message and the detail goes to the log.
		h.logger.Warn("failed to list provider models",
			zap.String("provider", providerName),
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"Could not reach that provider to list its models. Check the key and try again.", nil)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, list)
}

// accountMaySupplyKey answers whether this account may hold its own credential
// for a provider: the deployment must allow per-account keys at all, and the
// provider must support them.
//
// Both halves report through the same flag so the rest of the system has one
// thing to read. The write path already refuses an unsupported provider, so a
// deployment whose operator supplies the keys rejects stores without a second
// check, and the interface hides what it cannot offer without a second seam.
func accountMaySupplyKey(provider string) bool {
	return providerpolicy.AccountsSupplyKeys() && models.SupportsPerAccountKey(provider)
}

// ListUsage reports what each provider key is spent on and which model does
// each job. It exposes no account state and no credentials — it is a
// description of the build, identical for every caller.
func (h *Handler) ListUsage(w http.ResponseWriter, r *http.Request) {
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, agent.ProviderUsageCatalog())
}

type setKeyRequest struct {
	Key string `json:"key"`
}

type providerStatus struct {
	Provider string `json:"provider"`
	// Configured is true when this account can reach the provider, whether
	// through its own key or the deployment-level fallback.
	Configured bool `json:"configured"`
	// Source distinguishes the two, so the UI can say "using the server's key"
	// rather than implying the user supplied one.
	Source  string `json:"source"`
	KeyHint string `json:"key_hint,omitempty"`
	// Required marks providers the app cannot function without. OpenAI is
	// required because the supporting features — embeddings, summarization,
	// naming, mood, expressions, file attachments — call it regardless of
	// which chat model is selected.
	Required bool `json:"required"`
	// Supported is false for providers whose key cannot yet be supplied per
	// account. They are still listed so the screen shows the whole catalog,
	// but accepting a key we would not use would report the provider as
	// working and then fail at send time.
	Supported bool `json:"supported"`
}

// ListKeys reports every provider the catalog knows about and whether this
// account can use it. Reporting all of them, rather than only the configured
// ones, is what lets the UI offer the rest without a separate hardcoded list.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	stored, err := h.ds.ListUserProviderKeys(ctx, userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list provider keys", err)
		return
	}
	byProvider := make(map[string]datastore.UserProviderKeyInfo, len(stored))
	for _, s := range stored {
		byProvider[s.Provider] = s
	}

	out := make([]providerStatus, 0, len(models.CatalogProviders()))
	for _, p := range models.CatalogProviders() {
		name := string(p)
		st := providerStatus{
			Provider:  name,
			Required:  p == models.ModelProviderOpenAI,
			Supported: accountMaySupplyKey(name),
		}
		if info, ok := byProvider[name]; ok {
			st.Configured = true
			st.Source = "account"
			st.KeyHint = info.KeyHint
		} else if res := h.resolvers[name]; res != nil && res.Configured(ctx) {
			// No row of their own, but the deployment key covers them.
			st.Configured = true
			st.Source = "deployment"
		}
		out = append(out, st)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// SetKey stores or replaces this account's key for one provider.
func (h *Handler) SetKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(mux.Vars(r)["provider"]))
	if !models.IsCatalogProvider(provider) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Unknown provider", nil)
		return
	}
	if !accountMaySupplyKey(provider) {
		// Two reasons land here — the provider has no per-account support, or
		// this deployment's operator supplies the credentials — and the
		// response deliberately does not distinguish them. Storing the key
		// either way would report the provider as configured while its requests
		// used a different credential.
		//
		// No remedy in the text: what to do next differs by deployment, so it
		// belongs to whatever interface is talking to a person.
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet,
			"This account cannot supply its own key for that provider", nil)
		return
	}

	var req setKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Key is required", nil)
		return
	}

	info, err := h.ds.SetUserProviderKey(ctx, userID, provider, req.Key)
	if err != nil {
		// The key itself must never reach the log, and RespondWithError logs
		// the error it is given — so pass the failure, never the payload.
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to save provider key", err)
		return
	}
	if res := h.resolvers[provider]; res != nil {
		res.Invalidate(userID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(providerStatus{
		Provider:   info.Provider,
		Configured: true,
		Source:     "account",
		KeyHint:    info.KeyHint,
		Required:   provider == string(models.ModelProviderOpenAI),
		Supported:  true,
	})
}

// DeleteKey removes this account's key for one provider. The account may still
// be able to reach it afterwards through the deployment fallback, which the
// response reports rather than assuming.
func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(mux.Vars(r)["provider"]))
	if !models.IsCatalogProvider(provider) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Unknown provider", nil)
		return
	}

	if err := h.ds.DeleteUserProviderKey(ctx, userID, provider); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to remove provider key", err)
		return
	}
	res := h.resolvers[provider]
	if res != nil {
		res.Invalidate(userID)
	}

	st := providerStatus{
		Provider:  provider,
		Required:  provider == string(models.ModelProviderOpenAI),
		Supported: accountMaySupplyKey(provider),
	}
	if res != nil && res.Configured(ctx) {
		st.Configured = true
		st.Source = "deployment"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}
