package datastore

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createWebhookTokenTestSchema adds the tables webhook token creation and authentication touch:
// the tokens themselves, plus roles (authentication loads the owner's roles). Columns follow
// ent/migrate/schema.go; scopes is nullable because tokens created before it existed have none.
func createWebhookTokenTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE roles (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL UNIQUE,
			description text
		)`,
		`CREATE TABLE user_roles (
			user_id uuid NOT NULL,
			role_id uuid NOT NULL,
			PRIMARY KEY (user_id, role_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE webhook_tokens (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			token_hash text NOT NULL UNIQUE,
			status text NOT NULL DEFAULT 'active',
			last_used_at datetime,
			scopes json,
			user_webhook_tokens uuid NOT NULL,
			FOREIGN KEY (user_webhook_tokens) REFERENCES users(id) ON DELETE CASCADE
		)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newWebhookTokenTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createWebhookTokenTestSchema)
}

func createWebhookTokenTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("hook-" + id.String()[:8]).
		SetEmail("hook-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

// storedScopes reads the raw scopes column, to check what was persisted rather than what the
// model reports.
func storedScopes(t *testing.T, ds *Datastore, tokenID uuid.UUID) (scopes []string, isNull bool) {
	t.Helper()
	var raw sql.NullString
	require.NoError(t, ds.sqlDB.QueryRow(`SELECT scopes FROM webhook_tokens WHERE id = ?`, tokenID.String()).Scan(&raw))
	if !raw.Valid {
		return nil, true
	}
	require.NoError(t, json.Unmarshal([]byte(raw.String), &scopes))
	return scopes, false
}

func TestCreateWebhookToken_NoScopesRequestedStoresWriteOnly(t *testing.T) {
	ds, cleanup := newWebhookTokenTestDatastore(t)
	defer cleanup()
	userID := createWebhookTokenTestUser(t, ds)

	token, raw, err := ds.CreateWebhookToken(context.Background(), userID, "slack", nil)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.Equal(t, []models.WebhookScope{models.WebhookScopeMessagesWrite}, token.Scopes)

	stored, isNull := storedScopes(t, ds, token.ID)
	require.False(t, isNull, "a new token records its scopes explicitly")
	require.Equal(t, []string{"messages:write"}, stored)
}

func TestCreateWebhookToken_StoresAndAuthenticatesWithTheRequestedScopes(t *testing.T) {
	ds, cleanup := newWebhookTokenTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createWebhookTokenTestUser(t, ds)

	readOnly, rawRead, err := ds.CreateWebhookToken(ctx, userID, "bridge", []models.WebhookScope{models.WebhookScopeChatRead})
	require.NoError(t, err)
	both, rawBoth, err := ds.CreateWebhookToken(ctx, userID, "both", []models.WebhookScope{models.WebhookScopeMessagesWrite, models.WebhookScopeChatRead})
	require.NoError(t, err)
	require.Equal(t, []models.WebhookScope{models.WebhookScopeChatRead}, readOnly.Scopes)

	p, err := ds.AuthenticateWebhookToken(ctx, rawRead)
	require.NoError(t, err)
	require.Equal(t, userID, p.UserID)
	require.Equal(t, readOnly.ID, p.WebhookTokenID)
	require.True(t, p.HasScope(models.WebhookScopeChatRead))
	require.False(t, p.HasScope(models.WebhookScopeMessagesWrite), "a read-only token must not authenticate as able to write")

	p, err = ds.AuthenticateWebhookToken(ctx, rawBoth)
	require.NoError(t, err)
	require.Equal(t, both.ID, p.WebhookTokenID)
	require.True(t, p.HasScope(models.WebhookScopeChatRead) && p.HasScope(models.WebhookScopeMessagesWrite))
}

// TestWebhookToken_LegacyTokenKeepsPostingAndNeverReads is the upgrade guarantee: a token row
// created before scopes existed has NULL in the column. It must authenticate as write-only, so
// deploying this change gives no existing token any read access.
func TestWebhookToken_LegacyTokenKeepsPostingAndNeverReads(t *testing.T) {
	ds, cleanup := newWebhookTokenTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createWebhookTokenTestUser(t, ds)

	const rawLegacy = "wht_legacy-token-from-before-scopes"
	legacyID := uuid.New()
	now := time.Now().UTC()
	_, err := ds.sqlDB.Exec(
		`INSERT INTO webhook_tokens (id, created_at, updated_at, name, token_hash, status, scopes, user_webhook_tokens)
		 VALUES (?, ?, ?, 'old slack trigger', ?, 'active', NULL, ?)`,
		legacyID.String(), now, now, hashWebhookToken(rawLegacy), userID.String())
	require.NoError(t, err)
	_, isNull := storedScopes(t, ds, legacyID)
	require.True(t, isNull, "the fixture must really be a pre-scopes row")

	p, err := ds.AuthenticateWebhookToken(ctx, rawLegacy)
	require.NoError(t, err)
	require.True(t, p.HasScope(models.WebhookScopeMessagesWrite), "existing integrations keep posting")
	require.False(t, p.HasScope(models.WebhookScopeChatRead), "and do not gain read access")

	tokens, err := ds.ListWebhookTokens(ctx, userID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, []models.WebhookScope{models.WebhookScopeMessagesWrite}, tokens[0].Scopes, "listing reports the effective scopes")
}

func TestListWebhookTokens_ShowsEachTokensScopes(t *testing.T) {
	ds, cleanup := newWebhookTokenTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createWebhookTokenTestUser(t, ds)

	_, _, err := ds.CreateWebhookToken(ctx, userID, "poster", nil)
	require.NoError(t, err)
	_, _, err = ds.CreateWebhookToken(ctx, userID, "reader", []models.WebhookScope{models.WebhookScopeChatRead})
	require.NoError(t, err)

	tokens, err := ds.ListWebhookTokens(ctx, userID)
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	byName := map[string][]models.WebhookScope{}
	for _, tk := range tokens {
		byName[tk.Name] = tk.Scopes
	}
	require.Equal(t, []models.WebhookScope{models.WebhookScopeMessagesWrite}, byName["poster"])
	require.Equal(t, []models.WebhookScope{models.WebhookScopeChatRead}, byName["reader"])
}
