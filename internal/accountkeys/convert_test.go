package accountkeys

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent/providerconnection"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// storeOldKey writes a row the way builds before provider connections did.
func storeOldKey(t *testing.T, f fixture, userID uuid.UUID, provider models.ModelProvider, sealed, hint string, at time.Time) {
	t.Helper()
	_, err := f.db.Exec(
		`INSERT INTO account_provider_keys (id, created_at, updated_at, user_id, provider, key, hint) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.New(), at, at, userID, string(provider), sealed, hint)
	require.NoError(t, err)
}

func countOldKeys(t *testing.T, f fixture) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM account_provider_keys`).Scan(&n))
	return n
}

func TestConvertStoredKeys_MakesBuiltinConnections(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	storeOldKey(t, f, f.alice, models.ModelProviderOpenAI, "sealed:sk-alice-openai", "enai", at)
	storeOldKey(t, f, f.alice, models.ModelProviderGoogle, "sealed:alice-gemini-key", "-key", at)
	storeOldKey(t, f, f.bob, models.ModelProviderOpenAI, "sealed:sk-bob-openai1", "nai1", at)

	n, err := ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Zero(t, countOldKeys(t, f), "converted rows are removed")

	row, err := f.client.ProviderConnection.Query().
		Where(providerconnection.UserID(f.alice), providerconnection.Builtin("google")).
		Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "google", row.Preset)
	require.Equal(t, "Google Gemini", row.Name)
	require.Equal(t, "sealed:alice-gemini-key", row.Key, "ciphertext copied as it is")
	require.Equal(t, "-key", row.Hint)
	require.Empty(t, row.BaseURL)
	require.True(t, row.CreatedAt.Equal(at), "keeps when the key was stored")
	require.Equal(t, SourceConverted, row.Source)

	require.Equal(t, "sk-alice-openai", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
	require.Equal(t, "sk-bob-openai1", f.keys.Key(asUser(f.bob), models.ModelProviderOpenAI))
}

func TestConvertStoredKeys_RunsAgainHarmlessly(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	storeOldKey(t, f, f.alice, models.ModelProviderOpenAI, "sealed:sk-alice-openai", "enai", time.Now())

	_, err := ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	n, err := ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, 1, f.client.ProviderConnection.Query().CountX(context.Background()))
}

// A connection that already exists keeps its key over an older row for the
// same provider, as when another instance converted first.
func TestConvertStoredKeys_KeepsANewerConnection(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	storeOldKey(t, f, f.alice, models.ModelProviderOpenAI, "sealed:sk-older-key-1111", "1111", time.Now().Add(-time.Hour))
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-newer-key-9999")
	require.NoError(t, err)

	n, err := ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Zero(t, countOldKeys(t, f))
	require.Equal(t, "sk-newer-key-9999", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
	row, err := f.client.ProviderConnection.Query().Only(context.Background())
	require.NoError(t, err)
	require.Empty(t, row.Source, "the existing connection is untouched")
}

// A row changed after the connection was made, as by an instance still running
// the previous build mid-upgrade, gives the connection its key.
func TestConvertStoredKeys_TakesANewerRow(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-connection-key-1111")
	require.NoError(t, err)
	later := time.Now().Add(time.Hour)
	storeOldKey(t, f, f.alice, models.ModelProviderOpenAI, "sealed:sk-replaced-key-9999", "9999", later)

	_, err = ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	require.Zero(t, countOldKeys(t, f))
	require.Equal(t, "sk-replaced-key-9999", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
	row, err := f.client.ProviderConnection.Query().Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "9999", row.Hint)
	require.True(t, row.UpdatedAt.Equal(later))

	// Running again changes nothing: the connection is now as new as the row was.
	_, err = ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, "sk-replaced-key-9999", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
}

func TestConvertStoredKeys_NothingToConvert(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	n, err := ConvertStoredKeys(context.Background(), f.client, zap.NewNop())
	require.NoError(t, err)
	require.Zero(t, n)
}
