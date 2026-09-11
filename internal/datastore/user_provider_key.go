package datastore

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userproviderkey"
	"go.uber.org/zap"
)

// ErrProviderKeyNotFound is returned when an account has no key for a provider.
var ErrProviderKeyNotFound = errors.New("provider key not found")

// UserProviderKeyInfo describes a stored key without exposing it.
//
// Reads for display never carry the secret: the plaintext leaves the datastore
// only through GetUserProviderKey, which exists to serve outgoing provider
// requests. Anything user-facing gets the hint instead, which is enough to tell
// two accounts' keys apart and useless to anyone who intercepts it.
type UserProviderKeyInfo struct {
	Provider string `json:"provider"`
	KeyHint  string `json:"key_hint"`
}

// keyHint renders the last four characters of a key for display, e.g. "…q4f2".
// Short keys produce no hint rather than leaking most of themselves.
func keyHint(key string) string {
	k := strings.TrimSpace(key)
	if len(k) < 8 {
		return ""
	}
	return "…" + k[len(k)-4:]
}

// SetUserProviderKey stores (or replaces) one account's key for one provider.
//
// The schema's unique index on (owner, provider) makes replacement the only
// sensible semantic: a second key for the same provider is not a choice anyone
// would want to make per request.
func (d *Datastore) SetUserProviderKey(ctx context.Context, userID uuid.UUID, provider, key string) (UserProviderKeyInfo, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	key = strings.TrimSpace(key)
	if provider == "" {
		return UserProviderKeyInfo{}, errors.New("provider is required")
	}
	if key == "" {
		return UserProviderKeyInfo{}, errors.New("key is required")
	}

	ciphertext, err := d.encryptTokenForWrite(key)
	if err != nil {
		return UserProviderKeyInfo{}, err
	}
	hint := keyHint(key)

	existing, err := d.dbClient.UserProviderKey.Query().
		Where(userproviderkey.HasOwnerWith(user.ID(userID)), userproviderkey.ProviderEQ(provider)).
		Only(ctx)
	switch {
	case err == nil:
		if _, err := d.dbClient.UserProviderKey.UpdateOneID(existing.ID).
			SetEncryptedKey(ciphertext).
			SetKeyHint(hint).
			Save(ctx); err != nil {
			d.logger.Error("failed to update provider key", zap.String("provider", provider), zap.Error(err))
			return UserProviderKeyInfo{}, err
		}
	case ent.IsNotFound(err):
		if _, err := d.dbClient.UserProviderKey.Create().
			SetOwnerID(userID).
			SetProvider(provider).
			SetEncryptedKey(ciphertext).
			SetKeyHint(hint).
			Save(ctx); err != nil {
			d.logger.Error("failed to create provider key", zap.String("provider", provider), zap.Error(err))
			return UserProviderKeyInfo{}, err
		}
	default:
		d.logger.Error("failed to query provider key", zap.String("provider", provider), zap.Error(err))
		return UserProviderKeyInfo{}, err
	}

	return UserProviderKeyInfo{Provider: provider, KeyHint: hint}, nil
}

// GetUserProviderKey returns the decrypted key for one account and provider.
// Returns ErrProviderKeyNotFound when the account has not set one.
func (d *Datastore) GetUserProviderKey(ctx context.Context, userID uuid.UUID, provider string) (string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	row, err := d.dbClient.UserProviderKey.Query().
		Where(userproviderkey.HasOwnerWith(user.ID(userID)), userproviderkey.ProviderEQ(provider)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", ErrProviderKeyNotFound
		}
		return "", err
	}
	return d.decryptTokenForRead(row.EncryptedKey)
}

// ListUserProviderKeys returns which providers an account has configured, with
// hints rather than keys.
func (d *Datastore) ListUserProviderKeys(ctx context.Context, userID uuid.UUID) ([]UserProviderKeyInfo, error) {
	rows, err := d.dbClient.UserProviderKey.Query().
		Where(userproviderkey.HasOwnerWith(user.ID(userID))).
		Order(ent.Asc(userproviderkey.FieldProvider)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserProviderKeyInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, UserProviderKeyInfo{Provider: r.Provider, KeyHint: r.KeyHint})
	}
	return out, nil
}

// DeleteUserProviderKey removes one account's key for one provider. Deleting a
// key that is not there is not an error: the caller's intent is satisfied.
func (d *Datastore) DeleteUserProviderKey(ctx context.Context, userID uuid.UUID, provider string) error {
	provider = strings.TrimSpace(strings.ToLower(provider))
	_, err := d.dbClient.UserProviderKey.Delete().
		Where(userproviderkey.HasOwnerWith(user.ID(userID)), userproviderkey.ProviderEQ(provider)).
		Exec(ctx)
	return err
}
