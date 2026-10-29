package accountkeys

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/accountproviderkey"
	"github.com/theimaginaryfoundation/what-iff/ent/providerconnection"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// SourceConverted marks a connection made from a key stored before connections
// existed.
const SourceConverted = "converted"

// ConvertStoredKeys turns every key stored in the old account_provider_keys
// table into that account's built-in connection for the provider, and removes
// the old row (ADR 0x027). The ciphertext is copied as it is, never decrypted.
//
// It runs at startup and is safe to run again, or on two instances at once: a
// connection that already exists for the account and provider is kept, and the
// old row is removed either way. A deployment that supplies its own keys has no
// rows, so it does nothing there. It reports how many old rows it handled,
// including any whose connection already existed.
//
// It runs in one transaction. That suits the table it reads: at most one row
// per provider per account, written only on self-hosted installs, where
// accounts are few.
func ConvertStoredKeys(ctx context.Context, client *ent.Client, logger *zap.Logger) (int, error) {
	tx, err := client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin converting stored keys: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.AccountProviderKey.Query().All(ctx)
	if err != nil {
		return 0, fmt.Errorf("read stored keys: %w", err)
	}
	for _, row := range rows {
		provider := models.ModelProvider(row.Provider)
		err := tx.ProviderConnection.Create().
			SetUserID(row.UserID).
			SetPreset(row.Provider).
			SetBuiltin(row.Provider).
			SetName(provider.DisplayName()).
			SetKey(row.Key).
			SetHint(row.Hint).
			SetCreatedAt(row.CreatedAt).
			SetUpdatedAt(row.UpdatedAt).
			SetSource(SourceConverted).
			// A connection that already exists is kept as it is.
			OnConflictColumns(providerconnection.FieldUserID, providerconnection.FieldBuiltin).
			Ignore().
			Exec(ctx)
		if err != nil {
			return 0, fmt.Errorf("convert stored %s key for user %s: %w", row.Provider, row.UserID, err)
		}
	}
	if _, err := tx.AccountProviderKey.Delete().
		Where(accountproviderkey.IDIn(ids(rows)...)).
		Exec(ctx); err != nil {
		return 0, fmt.Errorf("remove converted keys: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit converted keys: %w", err)
	}
	if len(rows) > 0 {
		logger.Info("converted stored provider keys into connections", zap.Int("rows", len(rows)))
	}
	return len(rows), nil
}

func ids(rows []*ent.AccountProviderKey) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}
