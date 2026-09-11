// Package providerkey exposes an account's own model-provider API keys.
//
// Every route here is scoped to the authenticated caller: the account is taken
// from the request context, never from the payload, so one user cannot read or
// replace another's credential by guessing an id. There are no ids in these
// routes at all — an account has at most one key per provider.
package providerkey

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// Handler serves the per-account provider-key routes.
type Handler struct {
	ds        *datastore.Datastore
	logger    *zap.Logger
	resolvers map[string]*providerkeys.Resolver
}

// NewHandler builds the handler. resolvers are keyed by provider and are
// notified on write; a provider with no resolver still stores fine, it just
// has no cache to clear.
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, resolvers map[string]*providerkeys.Resolver) *Handler {
	return &Handler{ds: ds, logger: logger, resolvers: resolvers}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	r := router.PathPrefix("/provider-keys").Subrouter()
	r.HandleFunc("", h.ListKeys).Methods("GET")
	r.HandleFunc("/{provider}", h.SetKey).Methods("PUT")
	r.HandleFunc("/{provider}", h.DeleteKey).Methods("DELETE")
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
			Supported: models.SupportsPerAccountKey(name),
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
	if !models.SupportsPerAccountKey(provider) {
		// Storing it would report the provider as configured while its
		// requests still used the deployment credential, or failed outright.
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet,
			"Per-account keys are not supported for this provider yet; set it in the server environment instead", nil)
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
		Supported: models.SupportsPerAccountKey(provider),
	}
	if res != nil && res.Configured(ctx) {
		st.Configured = true
		st.Source = "deployment"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}
