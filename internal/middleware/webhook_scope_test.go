package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func scopeProbe(scope models.WebhookScope) (http.Handler, *bool) {
	reached := false
	h := RequireWebhookScope(scope, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	return h, &reached
}

func withScopes(scopes []models.WebhookScope) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return req.WithContext(context.WithValue(req.Context(), WebhookScopesKey, scopes))
}

func TestRequireWebhookScope_AllowsATokenThatHoldsIt(t *testing.T) {
	t.Parallel()

	h, reached := scopeProbe(models.WebhookScopeChatRead)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withScopes([]models.WebhookScope{models.WebhookScopeMessagesWrite, models.WebhookScopeChatRead}))

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, *reached)
}

func TestRequireWebhookScope_RefusesATokenThatLacksIt(t *testing.T) {
	t.Parallel()

	h, reached := scopeProbe(models.WebhookScopeChatRead)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withScopes([]models.WebhookScope{models.WebhookScopeMessagesWrite}))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "chat:read")
	require.False(t, *reached, "the handler must not run for a token without the scope")
}

// TestRequireWebhookScope_FailsClosedWithoutAuth: if the auth middleware was forgotten, no scopes
// are in the context, and that must mean "nothing allowed", never "everything".
func TestRequireWebhookScope_FailsClosedWithoutAuth(t *testing.T) {
	t.Parallel()

	for name, req := range map[string]*http.Request{
		"no scopes in context": httptest.NewRequest(http.MethodGet, "/", nil),
		"empty scopes":         withScopes(nil),
		"wrong value type":     httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.WithValue(context.Background(), WebhookScopesKey, "chat:read")),
	} {
		h, reached := scopeProbe(models.WebhookScopeChatRead)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code, name)
		require.False(t, *reached, name)
	}
}

func TestWebhookAuthMiddleware_PutsScopesInContext(t *testing.T) {
	t.Parallel()

	want := []models.WebhookScope{models.WebhookScopeChatRead}
	store := &mockWebhookAuthStore{
		authenticateFn: func(context.Context, string) (*models.WebhookAuthPrincipal, error) {
			return &models.WebhookAuthPrincipal{UserID: uuid.New(), WebhookTokenID: uuid.New(), Scopes: want}, nil
		},
	}
	var got []models.WebhookScope
	var ok bool
	handler := WebhookAuthMiddleware(store, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = GetWebhookScopesFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wht_abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, ok)
	require.Equal(t, want, got)
}
