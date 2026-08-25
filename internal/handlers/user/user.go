package user

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/userhooks"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type userStore interface {
	CreateUser(ctx context.Context, userReq models.UserRegisterRequest) (*models.UserResponse, *models.TokenPair, error)
	GetUserByCredentials(ctx context.Context, loginReq models.UserLoginRequest) (*models.UserResponse, *models.TokenPair, error)
	GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error)
	UpdateUserProfile(ctx context.Context, userID uuid.UUID, updateReq models.UpdateUserRequest) (*models.UserResponse, error)
	UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordReq models.UpdatePasswordRequest) error
	DeleteUser(ctx context.Context, userID uuid.UUID) error
	RefreshUserToken(ctx context.Context, refreshToken string) (*models.TokenPair, error)
	GetUserPreferences(ctx context.Context, userID uuid.UUID) (*models.UserPreferences, error)
	UpdateUserPreferences(ctx context.Context, userID uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error)
	GetUsageStats(ctx context.Context, startDate, endDate time.Time, userID uuid.UUID) (*models.UsageStats, error)
}

// Handler handles user-related API requests
type Handler struct {
	store         userStore
	logger        *zap.Logger
	allowedEmails []string
	environment   string
}

// NewHandler creates a new Handler instance.
func NewHandler(store userStore, logger *zap.Logger, allowedEmails []string, environment string) *Handler {
	return &Handler{
		store:         store,
		logger:        logger,
		allowedEmails: allowedEmails,
		environment:   strings.ToLower(strings.TrimSpace(environment)),
	}
}

// RegisterRoutes registers all profile-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/user/profile", h.GetProfile).Methods("GET")
	router.HandleFunc("/user/profile", h.UpdateProfile).Methods("PUT")
	router.HandleFunc("/user/password", h.UpdatePassword).Methods("PUT")
	router.HandleFunc("/user/delete", h.DeleteUser).Methods("DELETE")
	router.HandleFunc("/user/refresh", h.RefreshToken).Methods("POST")
	router.HandleFunc("/user/usage", h.GetUsageStats).Methods("GET")
	router.HandleFunc("/user/preferences", h.GetUserPreferences).Methods("GET")
	router.HandleFunc("/user/preferences", h.UpdateUserPreferences).Methods("PUT")
}

// Register handles user registration
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req models.UserRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}

	// Validate required fields
	if req.Username == "" || req.Email == "" || req.Password == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Username, email, and password are required", nil)
		return
	}

	// Registration is not gated on terms-of-service acceptance. Acceptance is
	// recorded only when a caller explicitly sets terms_accepted (the datastore
	// stamps terms_accepted_at only then), so a signup that provides none leaves
	// it null rather than recording consent that was never given.

	emailLower := strings.ToLower(strings.TrimSpace(req.Email))
	if h.environment == "prod" || h.environment == "production" {
		localPart, _, _ := strings.Cut(emailLower, "@")
		if strings.Contains(localPart, "+") {
			h.logger.Info("registration blocked - plus email aliases are not allowed in production", zap.String("email", req.Email))
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Email aliases with + are not allowed for signup", nil)
			return
		}
	}

	// Check email allowlist (if configured)
	if len(h.allowedEmails) > 0 {
		allowed := false
		for _, allowedEmail := range h.allowedEmails {
			if emailLower == allowedEmail {
				allowed = true
				break
			}
		}
		if !allowed {
			h.logger.Info("registration blocked - email not on allowlist", zap.String("email", req.Email))
			handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Registration is currently restricted. Contact support if you believe this is an error.", nil)
			return
		}
	}

	// Create user using datastore
	user, tokenPair, err := h.store.CreateUser(r.Context(), req)
	if err != nil {
		switch err {
		case datastore.ErrUsernameExists:
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Username already taken", nil)
		case datastore.ErrEmailExists:
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Email already registered", nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error creating user", err)
		}
		return
	}

	// Run any post-registration hook the build registered (nil in the
	// open-source build, so registration has no extra side effects).
	if userhooks.OnRegistered != nil {
		userhooks.OnRegistered(r.Context(), user)
	}

	// Create response
	res := models.LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		User:         *user,
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, res)
}

// Login handles user login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req models.UserLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}

	// Authenticate user using datastore
	user, tokenPair, err := h.store.GetUserByCredentials(r.Context(), req)
	if err != nil {
		if err == datastore.ErrInvalidCredentials {
			handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Invalid credentials", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error during login", err)
		}
		return
	}

	// Create response
	res := models.LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		User:         *user,
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, res)
}

// GetProfile handles getting the current user's profile
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get user profile using datastore
	user, err := h.store.GetUserByID(r.Context(), userID)
	if err != nil {
		if err == datastore.ErrUserNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error fetching user profile", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, user)
}

// UpdateProfile handles updating the current user's profile
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Parse request body
	var req models.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}

	// Update user profile using datastore
	user, err := h.store.UpdateUserProfile(r.Context(), userID, req)
	if err != nil {
		switch err {
		case datastore.ErrUserNotFound:
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
		case datastore.ErrEmailExists:
			handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Email already in use", nil)
		case datastore.ErrInvalidTimezone:
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error updating user profile", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, user)
}

// UpdatePassword handles updating the current user's password
func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Parse request body
	var req models.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}

	// Update password using datastore
	err := h.store.UpdateUserPassword(r.Context(), userID, req)
	if err != nil {
		switch err {
		case datastore.ErrUserNotFound:
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
		case datastore.ErrExternalPasswordUnsupported:
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Password is managed by your identity provider", nil)
		case datastore.ErrCurrentPassword:
			handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Current password is incorrect", nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error updating password", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

// DeleteUser handles deleting a user account
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Delete user using datastore
	err := h.store.DeleteUser(r.Context(), userID)
	if err != nil {
		if err == datastore.ErrUserNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error deleting user account", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]string{"message": "User account deleted successfully"})
}

// GetUserByID gets a user by ID (admin only)
func (h *Handler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from request (e.g., from URL path parameter)
	idStr := r.PathValue("id")
	if idStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "User ID is required", nil)
		return
	}

	// Parse the ID
	userID, err := uuid.Parse(idStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid user ID", err)
		return
	}

	// Get user using datastore
	user, err := h.store.GetUserByID(r.Context(), userID)
	if err != nil {
		if err == datastore.ErrUserNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "User not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error fetching user", err)
		}
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, user)
}

// RefreshToken handles refreshing an access token using a valid refresh token
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req models.RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request payload", err)
		return
	}

	// Refresh token using datastore
	tokenPair, err := h.store.RefreshUserToken(r.Context(), req.RefreshToken)
	if err != nil {
		if err == datastore.ErrUserNotFound || err == datastore.ErrInvalidCredentials {
			handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Invalid refresh token", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error refreshing token", err)
		}
		return
	}

	// Create response
	res := models.RefreshTokenResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, res)
}
