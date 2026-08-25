package middleware

// WebhookAuthMiddleware authenticates webhook routes using static bearer API tokens and
// sets WebhookTokenIDKey on the request context to identify the trusted server-to-server
// caller. Do not attach WebhookAuthMiddleware to user-facing HTTP handlers.

import (
	"context"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type webhookTokenAuthenticator interface {
	AuthenticateWebhookToken(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error)
}

// WebhookAuthMiddleware authenticates webhook routes using static bearer API tokens.
func WebhookAuthMiddleware(store webhookTokenAuthenticator, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			if authHeader == "" {
				handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
					"Authorization header missing", nil)
				return
			}

			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			if token == authHeader || token == "" {
				handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
					"Invalid authorization format. Expected 'Bearer {token}'", nil)
				return
			}

			principal, err := store.AuthenticateWebhookToken(r.Context(), token)
			if err != nil {
				if err == datastore.ErrWebhookTokenInvalid {
					handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
						"Invalid webhook token", nil)
					return
				}
				logger.Error("failed to authenticate webhook token", zap.Error(err))
				handlerutils.RespondWithError(w, logger, http.StatusInternalServerError, handlerutils.CodeNotSet,
					"Authentication failed. Please try again", nil)
				return
			}

			ctx := apicontext.WithUserID(r.Context(), principal.UserID)
			ctx = context.WithValue(ctx, UserIDKey, principal.UserID)
			ctx = context.WithValue(ctx, UserRoleKey, principal.Role)
			ctx = context.WithValue(ctx, WebhookTokenIDKey, principal.WebhookTokenID)
			if principal.Timezone != "" {
				ctx = context.WithValue(ctx, ClientTimezoneKey, principal.Timezone)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
