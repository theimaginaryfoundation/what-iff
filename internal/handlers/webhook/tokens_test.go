package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// createToken posts to CreateWebhookToken as the session user and returns the response and the
// scopes the provider was asked to store.
func createToken(t *testing.T, body string) (*httptest.ResponseRecorder, []models.WebhookScope) {
	t.Helper()
	var stored []models.WebhookScope
	p := &mockProvider{createTokenFn: func(_ context.Context, userID uuid.UUID, name string, scopes []models.WebhookScope) (*models.WebhookToken, string, error) {
		stored = scopes
		return &models.WebhookToken{ID: uuid.New(), UserID: userID, Name: name, Status: models.WebhookTokenStatusActive, Scopes: scopes}, "wht_secret", nil
	}}
	h := NewHandler(p, &mockAgent{}, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/webhook-tokens", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, testOwner))
	rec := httptest.NewRecorder()
	h.CreateWebhookToken(rec, req)
	return rec, stored
}

func TestCreateWebhookToken_DefaultsToWriteOnly(t *testing.T) {
	t.Parallel()

	rec, stored := createToken(t, `{"name":"slack trigger"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, []models.WebhookScope{models.WebhookScopeMessagesWrite}, stored,
		"omitting scopes must give the least a token needs, never read access")

	var resp struct {
		Token struct {
			Scopes []string `json:"scopes"`
		} `json:"token"`
		APIToken string `json:"api_token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"messages:write"}, resp.Token.Scopes, "the response shows what the token can do")
	require.Equal(t, "wht_secret", resp.APIToken)
}

func TestCreateWebhookToken_GrantsExactlyWhatWasAskedFor(t *testing.T) {
	t.Parallel()

	rec, stored := createToken(t, `{"name":"bridge","scopes":["chat:read"]}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, []models.WebhookScope{models.WebhookScopeChatRead}, stored, "read-only stays read-only")

	rec, stored = createToken(t, `{"name":"bridge","scopes":["chat:read","messages:write","chat:read"]}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, []models.WebhookScope{models.WebhookScopeMessagesWrite, models.WebhookScopeChatRead}, stored)
}

func TestCreateWebhookToken_RejectsUnknownScopes(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"name":"x","scopes":["admin"]}`,
		`{"name":"x","scopes":["chat:read","everything"]}`,
		`{"name":"x","scopes":[""]}`,
		`{"name":"x","scopes":"chat:read"}`,
	} {
		rec, stored := createToken(t, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
		require.Nil(t, stored, "nothing may be created for a request with an invalid scope: %s", body)
	}
}
