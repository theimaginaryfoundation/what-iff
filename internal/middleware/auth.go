package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/auth"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type contextKey string

const (
	UserIDKey         contextKey = "user_id"
	UserRoleKey       contextKey = "user_role"
	WebhookTokenIDKey contextKey = "webhook_token_id"
	// ClientTimezoneKey holds an optional IANA timezone name (e.g. "America/Los_Angeles") passed from the UI.
	ClientTimezoneKey contextKey = "client_timezone"
)

// AuthMiddleware authenticates requests via a bearer token. When authentication
// is delegated to an upstream provider, it provisions the user on first sign-in;
// otherwise it validates the built-in access token.
func AuthMiddleware(client *ent.Client, store *datastore.Datastore, logger *zap.Logger) func(http.Handler) http.Handler {
	var externalAuth auth.ExternalAuthenticator
	if auth.ExternalAuthenticatorProvider != nil {
		externalAuth = auth.ExternalAuthenticatorProvider(logger)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if externalAuth != nil {
				if id, ok := externalAuth.Authenticate(r); ok {
					// Normalize at the boundary; downstream provisioning expects
					// trimmed values keyed on a non-empty subject.
					id.Subject = strings.TrimSpace(id.Subject)
					id.Email = strings.TrimSpace(id.Email)
					id.Username = strings.TrimSpace(id.Username)
					if id.Subject == "" {
						logger.Error("External identity resolved with an empty subject; rejecting sign-in")
						handlerutils.RespondWithError(w, logger, http.StatusInternalServerError, handlerutils.CodeNotSet,
							"Sign-in failed. Please try again", nil)
						return
					}

					u, err := store.GetOrCreateExternalUser(r.Context(), id.Subject, id.Username, id.Email, id.FirstName, id.LastName)
					if err != nil {
						logger.Error("Failed to resolve user from external identity",
							zap.String("subject", id.Subject), zap.Error(err))
						switch {
						case errors.Is(err, datastore.ErrExternalEmailMissing):
							handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet,
								"We could not complete sign-in because your account is missing an email address", nil)
						case errors.Is(err, datastore.ErrExternalUsernameInvalid):
							handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet,
								"We could not complete sign-in because your account has an invalid username", nil)
						default:
							handlerutils.RespondWithError(w, logger, http.StatusInternalServerError, handlerutils.CodeNotSet,
								"Sign-in failed. Please try again", nil)
						}
						return
					}
					next.ServeHTTP(w, r.WithContext(applyUserToContext(r.Context(), u)))
					return
				}
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
					"Authorization header missing", nil)
				return
			}

			splitToken := strings.Split(authHeader, "Bearer ")
			if len(splitToken) != 2 {
				handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
					"Invalid authorization format. Expected 'Bearer {token}'", nil)
				return
			}
			tokenString := splitToken[1]

			claims, err := auth.ValidateAccessToken(tokenString)
			if err != nil {
				logger.Info("Invalid token", zap.Error(err))
				handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
					"Your session has expired. Please sign in again", nil)
				return
			}

			u, err := client.User.Query().
				Where(user.ID(claims.UserID)).
				WithRoles().
				Only(r.Context())
			if err != nil {
				logger.Info("Failed to get user for roles", zap.Error(err))
				handlerutils.RespondWithError(w, logger, http.StatusUnauthorized, handlerutils.CodeNotSet,
					"Your session is no longer valid. Please sign in again", nil)
				return
			}

			next.ServeHTTP(w, r.WithContext(applyUserToContext(r.Context(), u)))
		})
	}
}

// primaryRoleName returns the user's effective role, preferring admin/super_admin
// when the user holds several roles.
func primaryRoleName(u *ent.User) string {
	roleName := "user"
	if len(u.Edges.Roles) > 0 {
		for _, role := range u.Edges.Roles {
			if role.Name == "admin" || role.Name == "super_admin" {
				return role.Name
			}
		}
		roleName = u.Edges.Roles[0].Name
	}
	return roleName
}

// applyUserToContext stores the authenticated user's id, role, and timezone on ctx.
func applyUserToContext(ctx context.Context, u *ent.User) context.Context {
	ctx = apicontext.WithUserID(ctx, u.ID)
	ctx = context.WithValue(ctx, UserIDKey, u.ID)
	ctx = context.WithValue(ctx, UserRoleKey, primaryRoleName(u))
	if tz := strings.TrimSpace(u.Timezone); tz != "" {
		ctx = context.WithValue(ctx, ClientTimezoneKey, tz)
	}
	return ctx
}

// OptionalAuthMiddleware authenticates the request when credentials are present,
// but allows unauthenticated requests to continue without a user in context.
// Use for endpoints that behave differently for anonymous vs signed-in callers.
func OptionalAuthMiddleware(client *ent.Client, store *datastore.Datastore, logger *zap.Logger) func(http.Handler) http.Handler {
	requiredAuth := AuthMiddleware(client, store, logger)

	var externalAuth auth.ExternalAuthenticator
	if auth.ExternalAuthenticatorProvider != nil {
		externalAuth = auth.ExternalAuthenticatorProvider(logger)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hasExternal := false
			if externalAuth != nil {
				_, hasExternal = externalAuth.Authenticate(r)
			}
			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			if !hasExternal && authHeader == "" {
				next.ServeHTTP(w, r)
				return
			}
			requiredAuth(next).ServeHTTP(w, r)
		})
	}
}

// GetUserIDFromContext gets the user ID from the request context
func GetUserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if userID, ok := ctx.Value(UserIDKey).(uuid.UUID); ok {
		return userID, ok
	}
	return apicontext.UserIDFrom(ctx)
}

// GetUserRoleFromContext gets the user role from the request context.
func GetUserRoleFromContext(ctx context.Context) (string, bool) {
	// Role is stored as string (primary role from many-to-many relationship)
	roleValue := ctx.Value(UserRoleKey)
	if roleValue == nil {
		return "", false
	}

	// Role is always stored as string in the new schema
	if roleStr, ok := roleValue.(string); ok {
		return roleStr, true
	}

	// Fallback for any unexpected types (should not happen with new schema)
	return fmt.Sprintf("%v", roleValue), true
}

// GetClientTimezoneFromContext gets the client timezone from the request context.
func GetClientTimezoneFromContext(ctx context.Context) (string, bool) {
	tz, ok := ctx.Value(ClientTimezoneKey).(string)
	return tz, ok
}

// GetWebhookTokenIDFromContext gets the webhook token ID from the request context.
func GetWebhookTokenIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	tokenID, ok := ctx.Value(WebhookTokenIDKey).(uuid.UUID)
	return tokenID, ok
}

// CopyUserIDToContext sets the user ID in a new context from the request context
func CopyUserToIDContext(srcCtx, newCtx context.Context) (context.Context, bool) {
	userID, ok := srcCtx.Value(UserIDKey).(uuid.UUID)
	if !ok {
		return newCtx, false
	}

	out := apicontext.WithUserID(newCtx, userID)
	out = context.WithValue(out, UserIDKey, userID)
	// Best-effort: also carry over optional client timezone for downstream formatting (agent timestamps, etc.)
	if tz, ok := srcCtx.Value(ClientTimezoneKey).(string); ok {
		out = context.WithValue(out, ClientTimezoneKey, tz)
	}
	return out, true
}

// RequireRole creates a middleware that checks if the authenticated user has one of the allowed roles.
//
// logger is required because rejections go through handlerutils.RespondWithError,
// which logs every error response. It precedes the variadic roles.
func RequireRole(logger *zap.Logger, allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user role from context (should be set by AuthMiddleware)
			userRole, ok := GetUserRoleFromContext(r.Context())
			if !ok {
				handlerutils.RespondWithError(w, logger, http.StatusForbidden, handlerutils.CodeNotSet,
					"You do not have permission to access this resource", nil)
				return
			}

			// Check if user's primary role matches one of the allowed roles
			hasRole := false
			for _, role := range allowedRoles {
				if userRole == role {
					hasRole = true
					break
				}
			}

			if !hasRole {
				handlerutils.RespondWithError(w, logger, http.StatusForbidden, handlerutils.CodeNotSet,
					"You do not have permission to access this resource", nil)
				return
			}

			// User has required role, continue
			next.ServeHTTP(w, r)
		})
	}
}
