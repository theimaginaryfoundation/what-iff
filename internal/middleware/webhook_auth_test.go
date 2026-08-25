package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type mockWebhookAuthStore struct {
	authenticateFn func(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error)
}

func (m *mockWebhookAuthStore) AuthenticateWebhookToken(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error) {
	if m.authenticateFn != nil {
		return m.authenticateFn(ctx, rawToken)
	}
	return nil, datastore.ErrWebhookTokenInvalid
}

func TestWebhookAuthMiddleware_MissingAuthorizationHeader(t *testing.T) {
	t.Parallel()

	store := &mockWebhookAuthStore{}
	handler := WebhookAuthMiddleware(store, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhookAuthMiddleware_InvalidAuthorizationFormat(t *testing.T) {
	t.Parallel()

	store := &mockWebhookAuthStore{}
	handler := WebhookAuthMiddleware(store, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Token abc123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhookAuthMiddleware_InvalidToken(t *testing.T) {
	t.Parallel()

	store := &mockWebhookAuthStore{
		authenticateFn: func(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error) {
			return nil, datastore.ErrWebhookTokenInvalid
		},
	}
	handler := WebhookAuthMiddleware(store, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer revoked_or_invalid_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhookAuthMiddleware_InternalError(t *testing.T) {
	t.Parallel()

	store := &mockWebhookAuthStore{
		authenticateFn: func(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error) {
			return nil, errors.New("database unavailable")
		},
	}
	handler := WebhookAuthMiddleware(store, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWebhookAuthMiddleware_SetsPrincipalContext(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tokenID := uuid.New()
	store := &mockWebhookAuthStore{
		authenticateFn: func(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error) {
			require.Equal(t, "live_token", rawToken)
			return &models.WebhookAuthPrincipal{
				UserID:         userID,
				Role:           "user",
				Timezone:       "UTC",
				WebhookTokenID: tokenID,
			}, nil
		},
	}

	var gotUserID uuid.UUID
	var gotTokenID uuid.UUID
	var gotTimezone string
	handler := WebhookAuthMiddleware(store, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		gotUserID, ok = GetUserIDFromContext(r.Context())
		require.True(t, ok)
		gotTokenID, ok = GetWebhookTokenIDFromContext(r.Context())
		require.True(t, ok)
		gotTimezone, ok = GetClientTimezoneFromContext(r.Context())
		require.True(t, ok)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer live_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, userID, gotUserID)
	require.Equal(t, tokenID, gotTokenID)
	require.Equal(t, "UTC", gotTimezone)
}
