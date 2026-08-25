package datastore

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entrole "github.com/theimaginaryfoundation/what-iff/ent/role"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/auth"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Common user-related errors
var (
	ErrUserNotFound                = errors.New("user not found")
	ErrInvalidCredentials          = errors.New("invalid credentials")
	ErrUsernameExists              = errors.New("username already taken")
	ErrEmailExists                 = errors.New("email already registered")
	ErrExternalEmailMissing        = errors.New("external account missing email attribute")
	ErrExternalUsernameInvalid     = errors.New("external account missing valid username")
	ErrCurrentPassword             = errors.New("current password is incorrect")
	ErrExternalPasswordUnsupported = errors.New("password is managed by your identity provider")
	ErrInvalidTimezone             = errors.New("invalid IANA timezone identifier")
)

// validateIANATimezone checks that the given string is a valid IANA timezone name
// (e.g. "America/New_York"). It uses the Go standard library's time.LoadLocation,
// which relies on the IANA timezone database.
func validateIANATimezone(tz string) error {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return nil
	}
	_, err := time.LoadLocation(tz)
	if err != nil {
		return ErrInvalidTimezone
	}
	return nil
}

// getDefaultRoleID gets the default "user" role ID
func (d *Datastore) getDefaultRoleID(ctx context.Context, tx *ent.Tx) (uuid.UUID, error) {
	defaultRole, err := tx.Role.Query().
		Where(entrole.Name("user")).
		Only(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	return defaultRole.ID, nil
}

// Convert from Ent entity to model
func toUserModel(u *ent.User) *models.UserResponse {
	e := models.UserResponse{
		ID:                       u.ID,
		Username:                 u.Username,
		Email:                    u.Email,
		FirstName:                u.FirstName,
		LastName:                 u.LastName,
		Timezone:                 u.Timezone,
		Status:                   string(u.Status),
		EnableExperimentalModels: u.EnableExperimentalModels,
		CreatedAt:                u.CreatedAt,
		UpdatedAt:                u.UpdatedAt,
	}

	// Get primary role from many-to-many relationship
	// Prioritize "admin" role over "user" role if user has multiple roles
	if len(u.Edges.Roles) > 0 {
		// Check for admin role first (highest priority)
		foundAdmin := false
		for _, role := range u.Edges.Roles {
			if role.Name == "admin" || role.Name == "super_admin" {
				e.Role = role.Name
				foundAdmin = true
				break
			}
		}
		// If no admin role found, use the first role
		if !foundAdmin {
			e.Role = u.Edges.Roles[0].Name
		}
	} else {
		// Fallback to "user" if no roles are loaded
		e.Role = "user"
	}

	if u.Edges.Preferences != nil && u.Edges.Preferences.Theme.String() != "" {
		e.Theme = u.Edges.Preferences.Theme.String()
	} else {
		e.Theme = defaultTheme.String()
	}

	return &e
}

// CreateUser creates a new user in the database
func (d *Datastore) CreateUser(ctx context.Context, userReq models.UserRegisterRequest) (*models.UserResponse, *models.TokenPair, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, nil, err
	}

	// Rollback handling
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if username already exists
	exists, err := tx.User.Query().Where(user.Username(userReq.Username)).Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T("user.username_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	if exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, ErrUsernameExists
	}

	// Check if email already exists
	exists, err = tx.User.Query().Where(user.Email(userReq.Email)).Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T("user.email_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	if exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, ErrEmailExists
	}

	// Hash the password
	passwordHash, err := auth.HashPassword(userReq.Password)
	if err != nil {
		d.logger.Error(i18n.T("user.password_hash_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Get default role ID
	defaultRoleID, err := d.getDefaultRoleID(ctx, tx)
	if err != nil {
		d.logger.Error(i18n.T("user.default_role_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Create the user
	userBuilder := tx.User.Create().
		SetUsername(userReq.Username).
		SetEmail(userReq.Email).
		SetPasswordHash(passwordHash).
		SetFirstName(userReq.FirstName).
		SetLastName(userReq.LastName).
		AddRoleIDs(defaultRoleID)

	// Set terms acceptance timestamp if terms were accepted
	if userReq.TermsAccepted {
		userBuilder = userBuilder.SetTermsAcceptedAt(time.Now())
	}

	u, err := userBuilder.Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "user"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Load user with role for response
	u, err = tx.User.Query().
		Where(user.ID(u.ID)).
		WithRoles().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("user.load_with_role_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	defaultModel, err := resolveDefaultModelTx(ctx, tx)
	if err != nil {
		d.logger.Error(i18n.T1("user.default_model_failed", "Model", getDefaultModelName()), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Create user preferences
	_, err = tx.UserPreference.Create().
		SetUserID(u.ID).
		SetModelID(defaultModel.ID).
		SetTheme(defaultTheme).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "user preferences"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Run any linked per-user creation hook inside the transaction (nil in the
	// open-source build, so account creation has no extra side effects).
	if userCreatedTxHook != nil {
		if err := userCreatedTxHook(ctx, tx, u.ID); err != nil {
			d.logger.Error(i18n.T1("create.failed", "Entity", "user setup"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, nil, err
		}
	}

	// Generate tokens
	tokenPair, refreshTokenID, err := auth.GenerateTokenPair(u.ID)
	if err != nil {
		d.logger.Error(i18n.T("user.token_generate_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Update last login time and refresh token ID
	_, err = tx.User.UpdateOneID(u.ID).
		SetLastLogin(time.Now()).
		SetRefreshTokenID(refreshTokenID).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	if err != nil {
		d.logger.Warn(i18n.T("user.last_login_update_failed"), zap.Error(err))
		// Continue even if this fails as the user was created successfully
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, nil, err
	}

	return toUserModel(u), &models.TokenPair{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
	}, nil
}

func getDefaultModelName() string {
	if value := os.Getenv("DEFAULT_MODEL_NAME"); value != "" {
		return value
	}
	return models.DefaultModelName
}

// GetUserByCredentials gets a user by username/email and password
func (d *Datastore) GetUserByCredentials(ctx context.Context, credentials models.UserLoginRequest) (*models.UserResponse, *models.TokenPair, error) {
	// Try to find by username or email
	u, err := d.dbClient.User.Query().
		Where(
			user.Or(
				user.UsernameEqualFold(credentials.Username),
				user.EmailContainsFold(credentials.Username),
			),
		).
		WithPreferences().
		WithRoles().
		Only(ctx)

	if ent.IsNotFound(err) {
		return nil, nil, ErrInvalidCredentials
	}

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "user"), zap.Error(err))
		return nil, nil, err
	}

	// Check password
	if !auth.CheckPassword(credentials.Password, u.PasswordHash) {
		return nil, nil, ErrInvalidCredentials
	}

	// Generate tokens
	tokenPair, refreshTokenID, err := auth.GenerateTokenPair(u.ID)
	if err != nil {
		d.logger.Error(i18n.T("user.token_generate_failed"), zap.Error(err))
		return nil, nil, err
	}

	// Update last login time and refresh token ID
	_, err = d.dbClient.User.UpdateOneID(u.ID).
		SetLastLogin(time.Now()).
		SetRefreshTokenID(refreshTokenID).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	if err != nil {
		d.logger.Warn(i18n.T("user.last_login_update_failed"), zap.Error(err))
		// Continue even if this fails
	}

	return toUserModel(u), &models.TokenPair{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
	}, nil
}

// GetUserByID gets a user by ID
func (d *Datastore) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error) {
	u, err := d.dbClient.User.Query().
		Where(user.ID(userID)).
		WithPreferences().
		WithRoles().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		d.logger.Error(i18n.T("user.get_failed"), zap.Error(err))
		return nil, err
	}

	return toUserModel(u), nil
}

// UpdateUserProfile updates a user's profile
func (d *Datastore) UpdateUserProfile(ctx context.Context, userID uuid.UUID, profile models.UpdateUserRequest) (*models.UserResponse, error) {
	// Build update query
	update := d.dbClient.User.UpdateOneID(userID)
	update.SetUpdatedAt(time.Now())

	// Add fields to update if they're provided
	if profile.Email != "" {
		// Check if email already exists for another user
		exists, err := d.dbClient.User.Query().
			Where(
				user.Email(profile.Email),
				user.IDNEQ(userID),
			).
			Exist(ctx)

		if err != nil {
			d.logger.Error(i18n.T("user.email_check_failed"), zap.Error(err))
			return nil, err
		}

		if exists {
			return nil, ErrEmailExists
		}

		update.SetEmail(profile.Email)
	}

	if profile.FirstName != "" {
		update.SetFirstName(profile.FirstName)
	}

	if profile.LastName != "" {
		update.SetLastName(profile.LastName)
	}

	if profile.Timezone != "" {
		tz := strings.TrimSpace(profile.Timezone)
		if tz == "" {
			// Treat whitespace-only as a no-op.
		} else if err := validateIANATimezone(tz); err != nil {
			return nil, err
		} else {
			update.SetTimezone(tz)
		}
	}

	// Save the updates
	u, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		d.logger.Error(i18n.T1("update.failed", "Entity", "user"), zap.Error(err))
		return nil, err
	}

	prefs, err := d.dbClient.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("user.prefs_get_failed"), zap.Error(err))
		return nil, err
	}

	u.Edges.Preferences = prefs

	return toUserModel(u), nil
}

// UpdateUserPassword updates a user's password
func (d *Datastore) UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordReq models.UpdatePasswordRequest) error {
	// Get user from database
	u, err := d.dbClient.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		d.logger.Error(i18n.T("user.get_failed"), zap.Error(err))
		return err
	}

	// Provider-managed accounts do not have local passwords; surface a clear error.
	if u.PasswordHash == auth.ExternalAuthPasswordPlaceholder {
		return ErrExternalPasswordUnsupported
	}

	// Verify current password
	if !auth.CheckPassword(passwordReq.CurrentPassword, u.PasswordHash) {
		return ErrCurrentPassword
	}

	// Hash the new password
	passwordHash, err := auth.HashPassword(passwordReq.NewPassword)
	if err != nil {
		d.logger.Error(i18n.T("user.password_hash_failed"), zap.Error(err))
		return err
	}

	// Update the password
	_, err = d.dbClient.User.UpdateOne(u).
		SetPasswordHash(passwordHash).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T("user.password_update_failed"), zap.Error(err))
		return err
	}

	return nil
}

// DeleteUser deletes a user by ID
func (d *Datastore) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	err := d.dbClient.User.DeleteOneID(userID).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		d.logger.Error(i18n.T1("delete.failed", "Entity", "user"), zap.Error(err))
		return err
	}

	return nil
}

// RefreshUserToken refreshes a user's token
func (d *Datastore) RefreshUserToken(ctx context.Context, refreshToken string) (*models.TokenPair, error) {
	// Validate refresh token
	claims, err := auth.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// Get user and verify refresh token ID
	u, err := d.dbClient.User.Get(ctx, claims.UserID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		d.logger.Error(i18n.T("user.get_failed"), zap.Error(err))
		return nil, err
	}

	// Verify the refresh token ID matches
	if u.RefreshTokenID == "" || u.RefreshTokenID != claims.TokenID {
		return nil, ErrInvalidCredentials
	}

	// Generate new token pair
	tokenPair, refreshTokenID, err := auth.GenerateTokenPair(u.ID)
	if err != nil {
		d.logger.Error(i18n.T("user.token_generate_failed"), zap.Error(err))
		return nil, err
	}

	// Update refresh token ID
	_, err = d.dbClient.User.UpdateOneID(u.ID).
		SetRefreshTokenID(refreshTokenID).
		SetLastSeen(time.Now()).
		Save(ctx)

	if err != nil {
		d.logger.Warn(i18n.T("user.refresh_token_update_failed"), zap.Error(err))
		// Continue even if this fails
	}

	return &models.TokenPair{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
	}, nil
}

// getOrCreateExternalUser, when non-nil, provisions or links a user from an
// upstream identity provider's claims (Just-In-Time provisioning). It is nil in
// the open-source build — which has no external-identity integration and no
// cognito_sub field — so GetOrCreateExternalUser reports that external auth is
// not configured. A linked implementation sets it in an init() (see the overlay).
var getOrCreateExternalUser func(ctx context.Context, d *Datastore, cognitoSub, cognitoUsername, cognitoEmail, cognitoFirstName, cognitoLastName string) (*ent.User, error)

// ErrExternalAuthNotConfigured is returned when external-identity provisioning is
// requested in a build without an external-auth implementation linked.
var ErrExternalAuthNotConfigured = errors.New("external authentication is not configured")

// GetOrCreateExternalUser retrieves or creates a user by an upstream provider's
// subject claim, delegating to the linked external-auth implementation. The
// open-source build has none, so it returns ErrExternalAuthNotConfigured; the
// caller (AuthMiddleware) only reaches this path when an external authenticator
// is installed, which the same build does not provide.
func (d *Datastore) GetOrCreateExternalUser(ctx context.Context, cognitoSub string, cognitoUsername string, cognitoEmail string, cognitoFirstName string, cognitoLastName string) (*ent.User, error) {
	if getOrCreateExternalUser == nil {
		return nil, ErrExternalAuthNotConfigured
	}
	return getOrCreateExternalUser(ctx, d, cognitoSub, cognitoUsername, cognitoEmail, cognitoFirstName, cognitoLastName)
}
