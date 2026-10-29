// Package accountkeys holds the model-provider API keys accounts store
// themselves: their storage, the routes an account manages them through, and
// the resolver that answers a request with the requesting account's key.
//
// It is used when providerkeys.New picks it: then requests carry the account's
// key and the routes are mounted. Otherwise the table stays empty and no route
// is served.
package accountkeys

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerconnections"
	"github.com/theimaginaryfoundation/what-iff/internal/reauth"
)

var (
	ErrUnknownProvider = providerconnections.ErrUnknownProvider
	ErrInvalidKey      = providerconnections.ErrInvalidKey
	ErrKeyNotFound     = errors.New("no key stored for this provider")
)

// Sealer encrypts keys at rest with the deployment's token key.
type Sealer = providerconnections.Sealer

// Keys stores accounts' provider keys and answers requests with them.
type Keys struct {
	store   *providerconnections.Store
	confirm reauth.Confirmer
	logger  *zap.Logger
}

// New builds Keys over the database and the deployment's token key.
//
// confirm checks that whoever sets a key is the account's owner, not just
// someone holding its session.
func New(client *ent.Client, sealer Sealer, confirm reauth.Confirmer, logger *zap.Logger) *Keys {
	return &Keys{store: providerconnections.NewStore(client, sealer), confirm: confirm, logger: logger}
}

// --- Storage
//
// An account's key for a built-in provider is that provider's built-in
// connection (ADR 0x027), stored by internal/providerconnections.

func toSummary(c providerconnections.Connection) models.ProviderKeySummary {
	return models.ProviderKeySummary{
		Provider:  models.ModelProvider(c.Preset),
		Hint:      c.Hint,
		UpdatedAt: c.UpdatedAt,
	}
}

// Set stores the account's key for provider, replacing any key already stored
// for it.
func (k *Keys) Set(ctx context.Context, userID uuid.UUID, provider models.ModelProvider, key string) (models.ProviderKeySummary, error) {
	c, err := k.store.SetBuiltinKey(ctx, userID, provider, key)
	if err != nil {
		return models.ProviderKeySummary{}, err
	}
	return toSummary(c), nil
}

// List describes the account's stored keys, in provider order.
func (k *Keys) List(ctx context.Context, userID uuid.UUID) ([]models.ProviderKeySummary, error) {
	conns, err := k.store.Builtins(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]models.ProviderKeySummary, 0, len(conns))
	for _, c := range conns {
		out = append(out, toSummary(c))
	}
	return out, nil
}

// Delete removes the account's key for provider.
func (k *Keys) Delete(ctx context.Context, userID uuid.UUID, provider models.ModelProvider) error {
	err := k.store.DeleteBuiltin(ctx, userID, provider)
	if errors.Is(err, providerconnections.ErrNotFound) {
		return ErrKeyNotFound
	}
	return err
}

// stored returns the account's decrypted key for provider, or "" when it has
// none.
func (k *Keys) stored(ctx context.Context, userID uuid.UUID, provider models.ModelProvider) (string, error) {
	return k.store.BuiltinKey(ctx, userID, provider)
}

// --- Resolver (providerkeys.Resolver)

// Key returns the requesting account's key for provider, or "" when the
// context names no account, the account has no key, or it cannot be read.
// The account is the user ID on the context.
func (k *Keys) Key(ctx context.Context, provider models.ModelProvider) string {
	userID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		return ""
	}
	key, err := k.stored(ctx, userID, provider)
	if err != nil {
		k.logger.Error("could not read account provider key",
			zap.String("user_id", userID.String()),
			zap.String("provider", string(provider)),
			zap.Error(err))
		return ""
	}
	return key
}

// Configured is always true: which accounts have a key is only known per
// request, so every provider client is worth building.
func (k *Keys) Configured(models.ModelProvider) bool { return true }

// AccountsSupplyKeys is true: each account brings its own keys.
func (k *Keys) AccountsSupplyKeys() bool { return true }

// --- Routes. A key is write-only: no response, error or log line contains it.

// RegisterRoutes mounts the key routes on an authenticated router.
func (k *Keys) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/provider-keys", k.handleList).Methods(http.MethodGet)
	router.HandleFunc("/provider-keys/{provider}", k.handleSet).Methods(http.MethodPut)
	router.HandleFunc("/provider-keys/{provider}", k.handleDelete).Methods(http.MethodDelete)
}

func (k *Keys) requireAccount(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, k.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return userID, ok
}

func (k *Keys) handleList(w http.ResponseWriter, r *http.Request) {
	userID, ok := k.requireAccount(w, r)
	if !ok {
		return
	}
	keys, err := k.List(r.Context(), userID)
	if err != nil {
		handlerutils.RespondWithError(w, k.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list provider keys", err)
		return
	}
	handlerutils.RespondWithJSON(w, k.logger, http.StatusOK, keys)
}

func (k *Keys) handleSet(w http.ResponseWriter, r *http.Request) {
	userID, ok := k.requireAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Key string `json:"key"`
		// CurrentPassword confirms the change. A session alone is not enough to
		// redirect where the account's conversations are sent.
		CurrentPassword string `json:"current_password"`
	}
	if err := handlerutils.DecodeJSON(r, &req); err != nil {
		handlerutils.RespondWithError(w, k.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", nil)
		return
	}
	if err := k.confirm.Confirm(r.Context(), userID, req.CurrentPassword); err != nil {
		if errors.Is(err, reauth.ErrNotConfirmed) {
			handlerutils.RespondWithError(w, k.logger, http.StatusForbidden, models.ErrCodeReauthenticationRequired,
				"Confirm your current password to change a provider key", nil)
			return
		}
		handlerutils.RespondWithError(w, k.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to confirm the change", err)
		return
	}
	summary, err := k.Set(r.Context(), userID, models.ModelProvider(mux.Vars(r)["provider"]), req.Key)
	switch {
	case errors.Is(err, ErrUnknownProvider):
		handlerutils.RespondWithError(w, k.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Unknown provider", nil)
	case errors.Is(err, ErrInvalidKey):
		handlerutils.RespondWithError(w, k.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
	case err != nil:
		handlerutils.RespondWithError(w, k.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to store provider key", err)
	default:
		handlerutils.RespondWithJSON(w, k.logger, http.StatusOK, summary)
	}
}

func (k *Keys) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := k.requireAccount(w, r)
	if !ok {
		return
	}
	err := k.Delete(r.Context(), userID, models.ModelProvider(mux.Vars(r)["provider"]))
	switch {
	case errors.Is(err, ErrKeyNotFound):
		handlerutils.RespondWithError(w, k.logger, http.StatusNotFound, handlerutils.CodeNotSet, "No key stored for this provider", nil)
	case err != nil:
		handlerutils.RespondWithError(w, k.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete provider key", err)
	default:
		handlerutils.RespondWithNoContent(w)
	}
}
