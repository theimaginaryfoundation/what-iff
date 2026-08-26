package datastore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/auth"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createUserRoleTestSchema creates the roles and user_roles tables that user
// creation depends on: getDefaultRoleID looks up the "user" role by name, and
// CreateUser links the new user to it via AddRoleIDs. toUserModel's role-priority
// logic (admin over user) also depends on these tables being populated.
//
// Must be composed after createMemoryImportTestSchema (users is an FK parent here),
// e.g. newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema,
// createUserRoleTestSchema).
func createUserRoleTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	statements := []string{
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
			PRIMARY KEY (user_id, role_id)
		)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newUserTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createUserRoleTestSchema)
}

// setUserTestJWTSecrets sets the JWT env vars that auth.GenerateTokenPair and
// auth.ValidateRefreshToken require; they are unset by default in the test process.
func setUserTestJWTSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("JWT_REFRESH_SECRET", "test-jwt-refresh-secret")
}

func createUserTestRole(t *testing.T, ds *Datastore, name string) uuid.UUID {
	t.Helper()
	r, err := ds.dbClient.Role.Create().SetName(name).Save(context.Background())
	require.NoError(t, err)
	return r.ID
}

func createUserTestDefaultModel(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.Model.Create().
		SetName("gpt-test").
		SetDisplayName("GPT Test").
		SetDescription("test model").
		SetIsDefault(true).
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func TestValidateIANATimezone(t *testing.T) {
	tests := []struct {
		name    string
		tz      string
		wantErr error
	}{
		{name: "empty is a no-op", tz: "", wantErr: nil},
		{name: "whitespace-only is a no-op", tz: "   ", wantErr: nil},
		{name: "valid IANA name", tz: "America/New_York", wantErr: nil},
		{name: "invalid name", tz: "Not/A_Zone", wantErr: ErrInvalidTimezone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIANATimezone(tt.tz)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetDefaultModelName(t *testing.T) {
	t.Run("falls back to models.DefaultModelName when unset", func(t *testing.T) {
		t.Setenv("DEFAULT_MODEL_NAME", "")
		require.Equal(t, models.DefaultModelName, getDefaultModelName())
	})

	t.Run("uses DEFAULT_MODEL_NAME when set", func(t *testing.T) {
		t.Setenv("DEFAULT_MODEL_NAME", "custom-model")
		require.Equal(t, "custom-model", getDefaultModelName())
	})
}

func TestGetDefaultRoleID(t *testing.T) {
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()

	t.Run("role not found", func(t *testing.T) {
		tx, err := ds.dbClient.Tx(context.Background())
		require.NoError(t, err)
		defer tx.Rollback()

		_, err = ds.getDefaultRoleID(context.Background(), tx)
		require.Error(t, err)
	})

	roleID := createUserTestRole(t, ds, "user")

	t.Run("role found", func(t *testing.T) {
		tx, err := ds.dbClient.Tx(context.Background())
		require.NoError(t, err)
		defer tx.Rollback()

		got, err := ds.getDefaultRoleID(context.Background(), tx)
		require.NoError(t, err)
		require.Equal(t, roleID, got)
	})
}

func TestCreateUser(t *testing.T) {
	setUserTestJWTSecrets(t)

	t.Run("success", func(t *testing.T) {
		ds, cleanup := newUserTestDatastore(t)
		defer cleanup()
		createUserTestRole(t, ds, "user")
		createUserTestDefaultModel(t, ds)

		resp, tokens, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
			Username:      "newuser",
			Email:         "newuser@example.com",
			Password:      "supersecret",
			FirstName:     "New",
			LastName:      "User",
			TermsAccepted: true,
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		require.NotEmpty(t, tokens.AccessToken)
		require.NotEmpty(t, tokens.RefreshToken)
		require.Equal(t, "newuser", resp.Username)
		require.Equal(t, "newuser@example.com", resp.Email)
		require.Equal(t, "user", resp.Role)
		require.Equal(t, defaultTheme.String(), resp.Theme)
	})

	t.Run("username already exists", func(t *testing.T) {
		ds, cleanup := newUserTestDatastore(t)
		defer cleanup()
		createUserTestRole(t, ds, "user")
		createUserTestDefaultModel(t, ds)

		req := models.UserRegisterRequest{
			Username: "dupeuser",
			Email:    "one@example.com",
			Password: "supersecret",
		}
		_, _, err := ds.CreateUser(context.Background(), req)
		require.NoError(t, err)

		req.Email = "two@example.com"
		_, _, err = ds.CreateUser(context.Background(), req)
		require.ErrorIs(t, err, ErrUsernameExists)
	})

	t.Run("email already exists", func(t *testing.T) {
		ds, cleanup := newUserTestDatastore(t)
		defer cleanup()
		createUserTestRole(t, ds, "user")
		createUserTestDefaultModel(t, ds)

		req := models.UserRegisterRequest{
			Username: "userone",
			Email:    "same@example.com",
			Password: "supersecret",
		}
		_, _, err := ds.CreateUser(context.Background(), req)
		require.NoError(t, err)

		req.Username = "usertwo"
		_, _, err = ds.CreateUser(context.Background(), req)
		require.ErrorIs(t, err, ErrEmailExists)
	})

	t.Run("no default role configured", func(t *testing.T) {
		ds, cleanup := newUserTestDatastore(t)
		defer cleanup()
		createUserTestDefaultModel(t, ds)

		_, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
			Username: "norole",
			Email:    "norole@example.com",
			Password: "supersecret",
		})
		require.Error(t, err)
	})

	t.Run("no default model configured", func(t *testing.T) {
		ds, cleanup := newUserTestDatastore(t)
		defer cleanup()
		createUserTestRole(t, ds, "user")

		_, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
			Username: "nomodel",
			Email:    "nomodel@example.com",
			Password: "supersecret",
		})
		require.Error(t, err)
	})
}

func TestGetUserByCredentials(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()
	createUserTestRole(t, ds, "user")
	createUserTestDefaultModel(t, ds)

	_, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "creduser",
		Email:    "cred@example.com",
		Password: "correcthorse",
	})
	require.NoError(t, err)

	t.Run("by username", func(t *testing.T) {
		resp, tokens, err := ds.GetUserByCredentials(context.Background(), models.UserLoginRequest{
			Username: "creduser",
			Password: "correcthorse",
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		require.Equal(t, "creduser", resp.Username)
	})

	t.Run("by email", func(t *testing.T) {
		resp, _, err := ds.GetUserByCredentials(context.Background(), models.UserLoginRequest{
			Username: "cred@example.com",
			Password: "correcthorse",
		})
		require.NoError(t, err)
		require.Equal(t, "creduser", resp.Username)
	})

	t.Run("wrong password", func(t *testing.T) {
		_, _, err := ds.GetUserByCredentials(context.Background(), models.UserLoginRequest{
			Username: "creduser",
			Password: "wrong",
		})
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("unknown user", func(t *testing.T) {
		_, _, err := ds.GetUserByCredentials(context.Background(), models.UserLoginRequest{
			Username: "ghost",
			Password: "whatever",
		})
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})
}

func TestGetUserByID(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()
	createUserTestRole(t, ds, "user")
	createUserTestDefaultModel(t, ds)

	resp, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "idlookup",
		Email:    "idlookup@example.com",
		Password: "supersecret",
	})
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		got, err := ds.GetUserByID(context.Background(), resp.ID)
		require.NoError(t, err)
		require.Equal(t, "idlookup", got.Username)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := ds.GetUserByID(context.Background(), uuid.New())
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestUpdateUserProfile(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()
	createUserTestRole(t, ds, "user")
	createUserTestDefaultModel(t, ds)

	userA, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "profileA",
		Email:    "profileA@example.com",
		Password: "supersecret",
	})
	require.NoError(t, err)

	_, _, err = ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "profileB",
		Email:    "profileB@example.com",
		Password: "supersecret",
	})
	require.NoError(t, err)

	t.Run("updates names and timezone", func(t *testing.T) {
		got, err := ds.UpdateUserProfile(context.Background(), userA.ID, models.UpdateUserRequest{
			FirstName: "Ada",
			LastName:  "Lovelace",
			Timezone:  "America/New_York",
		})
		require.NoError(t, err)
		require.Equal(t, "Ada", got.FirstName)
		require.Equal(t, "Lovelace", got.LastName)
		require.Equal(t, "America/New_York", got.Timezone)
	})

	t.Run("email conflict with another user", func(t *testing.T) {
		_, err := ds.UpdateUserProfile(context.Background(), userA.ID, models.UpdateUserRequest{
			Email: "profileB@example.com",
		})
		require.ErrorIs(t, err, ErrEmailExists)
	})

	t.Run("email changed to a free address", func(t *testing.T) {
		got, err := ds.UpdateUserProfile(context.Background(), userA.ID, models.UpdateUserRequest{
			Email: "profileA-new@example.com",
		})
		require.NoError(t, err)
		require.Equal(t, "profileA-new@example.com", got.Email)
	})

	t.Run("invalid timezone", func(t *testing.T) {
		_, err := ds.UpdateUserProfile(context.Background(), userA.ID, models.UpdateUserRequest{
			Timezone: "Not/A_Zone",
		})
		require.ErrorIs(t, err, ErrInvalidTimezone)
	})

	t.Run("whitespace-only timezone is a no-op", func(t *testing.T) {
		before, err := ds.GetUserByID(context.Background(), userA.ID)
		require.NoError(t, err)

		got, err := ds.UpdateUserProfile(context.Background(), userA.ID, models.UpdateUserRequest{
			Timezone: "   ",
		})
		require.NoError(t, err)
		require.Equal(t, before.Timezone, got.Timezone)
	})

	t.Run("user not found", func(t *testing.T) {
		_, err := ds.UpdateUserProfile(context.Background(), uuid.New(), models.UpdateUserRequest{
			FirstName: "Ghost",
		})
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestUpdateUserPassword(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()
	createUserTestRole(t, ds, "user")
	createUserTestDefaultModel(t, ds)

	resp, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "pwuser",
		Email:    "pwuser@example.com",
		Password: "originalpass",
	})
	require.NoError(t, err)

	t.Run("wrong current password", func(t *testing.T) {
		err := ds.UpdateUserPassword(context.Background(), resp.ID, models.UpdatePasswordRequest{
			CurrentPassword: "notright",
			NewPassword:     "newpass123",
		})
		require.ErrorIs(t, err, ErrCurrentPassword)
	})

	t.Run("success", func(t *testing.T) {
		err := ds.UpdateUserPassword(context.Background(), resp.ID, models.UpdatePasswordRequest{
			CurrentPassword: "originalpass",
			NewPassword:     "newpass123",
		})
		require.NoError(t, err)

		// Old password should no longer work, new one should.
		_, _, err = ds.GetUserByCredentials(context.Background(), models.UserLoginRequest{
			Username: "pwuser",
			Password: "originalpass",
		})
		require.ErrorIs(t, err, ErrInvalidCredentials)

		_, _, err = ds.GetUserByCredentials(context.Background(), models.UserLoginRequest{
			Username: "pwuser",
			Password: "newpass123",
		})
		require.NoError(t, err)
	})

	t.Run("user not found", func(t *testing.T) {
		err := ds.UpdateUserPassword(context.Background(), uuid.New(), models.UpdatePasswordRequest{
			CurrentPassword: "x",
			NewPassword:     "y",
		})
		require.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("external auth account", func(t *testing.T) {
		u, err := ds.dbClient.User.Create().
			SetUsername("externaluser").
			SetEmail("external@example.com").
			SetPasswordHash(auth.ExternalAuthPasswordPlaceholder).
			Save(context.Background())
		require.NoError(t, err)

		err = ds.UpdateUserPassword(context.Background(), u.ID, models.UpdatePasswordRequest{
			CurrentPassword: "irrelevant",
			NewPassword:     "newpass123",
		})
		require.ErrorIs(t, err, ErrExternalPasswordUnsupported)
	})
}

func TestDeleteUser(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()
	createUserTestRole(t, ds, "user")
	createUserTestDefaultModel(t, ds)

	resp, _, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "deleteme",
		Email:    "deleteme@example.com",
		Password: "supersecret",
	})
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		err := ds.DeleteUser(context.Background(), resp.ID)
		require.NoError(t, err)

		_, err = ds.GetUserByID(context.Background(), resp.ID)
		require.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("not found", func(t *testing.T) {
		err := ds.DeleteUser(context.Background(), uuid.New())
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestRefreshUserToken(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()
	createUserTestRole(t, ds, "user")
	createUserTestDefaultModel(t, ds)

	_, tokens, err := ds.CreateUser(context.Background(), models.UserRegisterRequest{
		Username: "refreshuser",
		Email:    "refreshuser@example.com",
		Password: "supersecret",
	})
	require.NoError(t, err)

	t.Run("invalid token string", func(t *testing.T) {
		_, err := ds.RefreshUserToken(context.Background(), "not-a-jwt")
		require.Error(t, err)
	})

	t.Run("success", func(t *testing.T) {
		newTokens, err := ds.RefreshUserToken(context.Background(), tokens.RefreshToken)
		require.NoError(t, err)
		require.NotEmpty(t, newTokens.AccessToken)
		require.NotEmpty(t, newTokens.RefreshToken)
	})

	t.Run("reused (rotated) refresh token is rejected", func(t *testing.T) {
		// The first refresh above already rotated the stored refresh_token_id,
		// so re-using the original refresh token must now fail.
		_, err := ds.RefreshUserToken(context.Background(), tokens.RefreshToken)
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})
}

func TestGetOrCreateExternalUser(t *testing.T) {
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()

	_, err := ds.GetOrCreateExternalUser(context.Background(), "sub-123", "extuser", "ext@example.com", "Ext", "User")
	require.ErrorIs(t, err, ErrExternalAuthNotConfigured)
}

func TestToUserModel(t *testing.T) {
	setUserTestJWTSecrets(t)
	ds, cleanup := newUserTestDatastore(t)
	defer cleanup()

	adminRoleID := createUserTestRole(t, ds, "admin")
	userRoleID := createUserTestRole(t, ds, "user")

	t.Run("admin role takes priority over user role", func(t *testing.T) {
		u, err := ds.dbClient.User.Create().
			SetUsername("multirole").
			SetEmail("multirole@example.com").
			SetPasswordHash("hash").
			AddRoleIDs(userRoleID, adminRoleID).
			Save(context.Background())
		require.NoError(t, err)

		loaded, err := ds.dbClient.User.Query().Where(user.ID(u.ID)).WithRoles().Only(context.Background())
		require.NoError(t, err)

		resp := toUserModel(loaded)
		require.Equal(t, "admin", resp.Role)
	})

	t.Run("no roles falls back to user", func(t *testing.T) {
		u, err := ds.dbClient.User.Create().
			SetUsername("norole2").
			SetEmail("norole2@example.com").
			SetPasswordHash("hash").
			Save(context.Background())
		require.NoError(t, err)

		resp := toUserModel(u)
		require.Equal(t, "user", resp.Role)
		require.Equal(t, defaultTheme.String(), resp.Theme)
	})
}
