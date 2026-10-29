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
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/providerconnection"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/reauth"
)

// Bounds on a stored key. Real provider keys sit well inside them; the minimum
// keeps the four-character hint from being the whole key.
const (
	minKeyLen = 8
	maxKeyLen = 512
)

var (
	ErrUnknownProvider = errors.New("unknown model provider")
	ErrInvalidKey      = errors.New("provider key must be 8 to 512 characters, without spaces")
	ErrKeyNotFound     = errors.New("no key stored for this provider")
)

// Sealer encrypts keys at rest with the deployment's token key.
type Sealer interface {
	EncryptToken(plaintext string) (string, error)
	DecryptToken(ciphertext string) (string, error)
}

// Keys stores accounts' provider keys and answers requests with them.
type Keys struct {
	client  *ent.Client
	sealer  Sealer
	confirm reauth.Confirmer
	logger  *zap.Logger
}

// New builds Keys over the database and the deployment's token key.
//
// confirm checks that whoever sets a key is the account's owner, not just
// someone holding its session.
func New(client *ent.Client, sealer Sealer, confirm reauth.Confirmer, logger *zap.Logger) *Keys {
	return &Keys{client: client, sealer: sealer, confirm: confirm, logger: logger}
}

// --- Storage
//
// An account's key for a built-in provider is that provider's built-in
// connection (ADR 0x027): one per provider per account, named after it.

func toSummary(row *ent.ProviderConnection) models.ProviderKeySummary {
	return models.ProviderKeySummary{
		Provider:  models.ModelProvider(row.Preset),
		Hint:      row.Hint,
		UpdatedAt: row.UpdatedAt,
	}
}

// builtinConnection selects the account's connection for a built-in provider.
func builtinConnection(userID uuid.UUID, provider models.ModelProvider) predicate.ProviderConnection {
	return providerconnection.And(
		providerconnection.UserID(userID),
		providerconnection.Builtin(string(provider)),
	)
}

// Set stores the account's key for provider, replacing any key already stored
// for it.
func (k *Keys) Set(ctx context.Context, userID uuid.UUID, provider models.ModelProvider, key string) (models.ProviderKeySummary, error) {
	if !provider.Known() {
		return models.ProviderKeySummary{}, ErrUnknownProvider
	}
	key = strings.TrimSpace(key)
	if len(key) < minKeyLen || len(key) > maxKeyLen || strings.ContainsFunc(key, unicode.IsSpace) {
		return models.ProviderKeySummary{}, ErrInvalidKey
	}
	enc, err := k.sealer.EncryptToken(key)
	if err != nil {
		return models.ProviderKeySummary{}, fmt.Errorf("encrypt provider key: %w", err)
	}

	id, err := k.client.ProviderConnection.Create().
		SetUserID(userID).
		SetPreset(string(provider)).
		SetBuiltin(string(provider)).
		SetName(provider.DisplayName()).
		SetKey(enc).
		SetHint(key[len(key)-4:]).
		OnConflictColumns(providerconnection.FieldUserID, providerconnection.FieldBuiltin).
		UpdateKey().
		UpdateHint().
		UpdateUpdatedAt().
		ID(ctx)
	if err != nil {
		return models.ProviderKeySummary{}, fmt.Errorf("store provider key: %w", err)
	}
	row, err := k.client.ProviderConnection.Get(ctx, id)
	if err != nil {
		return models.ProviderKeySummary{}, fmt.Errorf("read stored provider key: %w", err)
	}
	return toSummary(row), nil
}

// List describes the account's stored keys, in provider order.
func (k *Keys) List(ctx context.Context, userID uuid.UUID) ([]models.ProviderKeySummary, error) {
	rows, err := k.client.ProviderConnection.Query().
		Where(providerconnection.UserID(userID), providerconnection.BuiltinNotNil()).
		Order(ent.Asc(providerconnection.FieldBuiltin)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list provider keys: %w", err)
	}
	out := make([]models.ProviderKeySummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSummary(row))
	}
	return out, nil
}

// Delete removes the account's key for provider.
func (k *Keys) Delete(ctx context.Context, userID uuid.UUID, provider models.ModelProvider) error {
	n, err := k.client.ProviderConnection.Delete().
		Where(builtinConnection(userID, provider)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete provider key: %w", err)
	}
	if n == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// stored returns the account's decrypted key for provider, or "" when it has
// none.
func (k *Keys) stored(ctx context.Context, userID uuid.UUID, provider models.ModelProvider) (string, error) {
	row, err := k.client.ProviderConnection.Query().
		Where(builtinConnection(userID, provider)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read provider key: %w", err)
	}
	if row.Key == "" {
		return "", nil
	}
	key, err := k.sealer.DecryptToken(row.Key)
	if err != nil {
		return "", fmt.Errorf("decrypt provider key: %w", err)
	}
	return key, nil
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
