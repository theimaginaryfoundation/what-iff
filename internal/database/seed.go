package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/model"
	entrole "github.com/theimaginaryfoundation/what-iff/ent/role"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/auth"
	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var legacyModelNames = []string{
	appmodels.LegacyFineTunedModelName,
}

// EnsureSeedData makes sure critical reference data exists before serving requests.
func EnsureSeedData(ctx context.Context, client *ent.Client, logger *zap.Logger) error {
	// Seed roles first (required for user creation)
	defaultUserRole, defaultAdminRole, err := ensureRoles(ctx, client, logger)
	if err != nil {
		return err
	}

	// Seed all available models
	if err := ensureAllModels(ctx, client, logger); err != nil {
		return err
	}

	// Get the default model for migration purposes
	defaultModelName := envOrDefault("DEFAULT_MODEL_NAME", appmodels.DefaultModelName)
	defaultModel, err := client.Model.Query().
		Where(
			model.Name(defaultModelName),
			model.Deleted(false),
		).
		Only(ctx)
	if err != nil {
		return errors.New("default model not found after seeding: " + defaultModelName)
	}

	// Migrate any legacy models to the default
	if err := migrateLegacyModels(ctx, client, logger, defaultModel); err != nil {
		return err
	}

	// Store default role IDs for later use (can be accessed via GetDefaultRole)
	_ = defaultUserRole
	_ = defaultAdminRole

	return nil
}

func ensureAllModels(ctx context.Context, client *ent.Client, logger *zap.Logger) error {
	for _, modelConfig := range appmodels.AvailableModels {
		existing, err := client.Model.Query().Where(model.Name(modelConfig.Name)).Only(ctx)
		if err == nil {
			if existing.Deleted {
				logger.Info("Skipping seed for soft-deleted model", zap.String("name", modelConfig.Name))
				continue
			}
			desiredProvider := model.Provider(modelConfig.Provider)
			if existing.Provider != desiredProvider {
				if _, err := client.Model.UpdateOneID(existing.ID).
					SetProvider(desiredProvider).
					Save(ctx); err != nil {
					return err
				}
				logger.Info("Updated model provider from seed catalog",
					zap.String("name", modelConfig.Name),
					zap.String("provider", string(modelConfig.Provider)),
				)
			}
			continue
		}

		if !ent.IsNotFound(err) {
			return err
		}

		// Model doesn't exist - create it
		_, err = client.Model.Create().
			SetName(modelConfig.Name).
			SetDisplayName(modelConfig.DisplayName).
			SetDescription(modelConfig.Description).
			SetProvider(model.Provider(modelConfig.Provider)).
			SetToolSupport(modelConfig.ToolSupport).
			Save(ctx)
		if err != nil {
			return err
		}

		logger.Info("Seeded model", zap.String("name", modelConfig.Name))
	}

	return nil
}

func migrateLegacyModels(ctx context.Context, client *ent.Client, logger *zap.Logger, defaultModel *ent.Model) error {
	if defaultModel == nil {
		return nil
	}

	legacyModels, err := client.Model.Query().
		Where(
			model.NameIn(legacyModelNames...),
			model.Deleted(false),
		).
		All(ctx)
	if err != nil {
		return err
	}

	if len(legacyModels) == 0 {
		return nil
	}

	legacyIDs := make([]uuid.UUID, 0, len(legacyModels))
	legacyNames := make([]string, 0, len(legacyModels))
	for _, m := range legacyModels {
		legacyIDs = append(legacyIDs, m.ID)
		legacyNames = append(legacyNames, m.Name)
	}

	if _, err := client.UserPreference.Update().
		Where(userpreference.DefaultModelIn(legacyIDs...)).
		SetDefaultModel(defaultModel.ID).
		Save(ctx); err != nil {
		return err
	}

	if _, err := client.Chat.Update().
		Where(chat.HasModelWith(model.IDIn(legacyIDs...))).
		SetModelID(defaultModel.ID).
		Save(ctx); err != nil {
		return err
	}

	if _, err := client.Model.Update().
		Where(model.IDIn(legacyIDs...)).
		SetDeleted(true).
		Save(ctx); err != nil {
		return err
	}

	logger.Info("Migrated legacy models to default", zap.Strings("legacy_names", legacyNames), zap.String("default", defaultModel.Name))
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// ensureRoles creates initial roles (user and admin) if they don't exist
// Returns the default user role and admin role
func ensureRoles(ctx context.Context, client *ent.Client, logger *zap.Logger) (*ent.Role, *ent.Role, error) {
	// Start transaction
	tx, err := client.Tx(ctx)
	if err != nil {
		logger.Error("failed to start transaction for role seeding", zap.Error(err))
		return nil, nil, err
	}

	// Rollback handling
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Ensure "user" role exists
	userRole, err := tx.Role.Query().Where(entrole.Name("user")).Only(ctx)
	if ent.IsNotFound(err) {
		// Create user role
		userRole, err = tx.Role.Create().
			SetName("user").
			SetDescription("Default user role with standard permissions").
			Save(ctx)
		if err != nil {
			logger.Error("failed to create user role", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				logger.Error("failed to rollback transaction", zap.Error(rerr))
			}
			return nil, nil, err
		}
	} else if err != nil {
		logger.Error("failed to query user role", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Ensure "admin" role exists
	adminRole, err := tx.Role.Query().Where(entrole.Name("admin")).Only(ctx)
	if ent.IsNotFound(err) {
		// Create admin role
		adminRole, err = tx.Role.Create().
			SetName("admin").
			SetDescription("Administrator role with elevated permissions").
			Save(ctx)
		if err != nil {
			logger.Error("failed to create admin role", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				logger.Error("failed to rollback transaction", zap.Error(rerr))
			}
			return nil, nil, err
		}
	} else if err != nil {
		logger.Error("failed to query admin role", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return nil, nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		logger.Error("failed to commit role seeding transaction", zap.Error(err))
		return nil, nil, err
	}

	// Reload roles from committed transaction to get fresh instances
	userRole, err = client.Role.Query().Where(entrole.Name("user")).Only(ctx)
	if err != nil {
		return nil, nil, err
	}

	adminRole, err = client.Role.Query().Where(entrole.Name("admin")).Only(ctx)
	if err != nil {
		return nil, nil, err
	}

	return userRole, adminRole, nil
}

// AdminProvisionResult reports what CreateOrPromoteAdmin did.
type AdminProvisionResult string

const (
	// AdminProvisionNone is the zero value, returned alongside a non-nil error;
	// callers must not treat it as a completed outcome.
	AdminProvisionNone AdminProvisionResult = ""
	// AdminProvisionCreated means a new admin user was created.
	AdminProvisionCreated AdminProvisionResult = "created"
	// AdminProvisionPromoted means an existing user gained the admin role
	// (password reset when requested and not provider-managed).
	AdminProvisionPromoted AdminProvisionResult = "promoted"
	// AdminProvisionAlreadyAdmin means the user already had the admin role and
	// nothing changed.
	AdminProvisionAlreadyAdmin AdminProvisionResult = "already_admin"
)

// CreateOrPromoteAdmin provisions an admin user with explicit
// create/promote/password-reset semantics, resolving the admin role and
// default model itself. Used by cmd/create-superuser (`make local-superuser`),
// which is the ONLY admin-provisioning entry point: the former SUPERADMIN_*
// boot path (ensureSuperAdmin) was removed after a security review flagged
// it, so no environment variable can mint an admin in a running deployment.
// CI test users come from the public register endpoint.
func CreateOrPromoteAdmin(ctx context.Context, client *ent.Client, logger *zap.Logger, email, password string) (AdminProvisionResult, error) {
	adminRole, err := client.Role.Query().Where(entrole.Name("admin")).Only(ctx)
	if err != nil {
		return AdminProvisionNone, fmt.Errorf("admin role not found; run seeding (EnsureSeedData) first: %w", err)
	}
	defaultModelName := envOrDefault("DEFAULT_MODEL_NAME", appmodels.DefaultModelName)
	defaultModel, err := client.Model.Query().
		Where(model.Name(defaultModelName), model.Deleted(false)).
		Only(ctx)
	if err != nil {
		return AdminProvisionNone, fmt.Errorf("default model %q not found; run seeding (EnsureSeedData) first: %w", defaultModelName, err)
	}
	return createOrPromoteAdmin(ctx, client, logger, defaultModel, adminRole, email, password)
}

// createOrPromoteAdmin creates the user with the admin role, or promotes an
// existing user (resetting the password unless it is provider-managed). A user
// that already holds the admin role is left untouched.
func createOrPromoteAdmin(ctx context.Context, client *ent.Client, logger *zap.Logger, defaultModel *ent.Model, adminRole *ent.Role, superAdminEmail, superAdminPassword string) (AdminProvisionResult, error) {
	// Start transaction
	tx, err := client.Tx(ctx)
	if err != nil {
		logger.Error("failed to start transaction for admin creation", zap.Error(err))
		return AdminProvisionNone, err
	}

	// Rollback handling
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if a user with this email already exists
	existingUser, err := tx.User.Query().
		Where(user.EmailEqualFold(superAdminEmail)).
		Only(ctx)

	if err == nil {
		// User exists - check if they have admin role
		existingUserWithRoles, err := tx.User.Query().
			Where(user.ID(existingUser.ID)).
			WithRoles().
			Only(ctx)
		if err != nil {
			logger.Error("failed to load user roles", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				logger.Error("failed to rollback transaction", zap.Error(rerr))
			}
			return AdminProvisionNone, err
		}

		// Check if user already has admin role
		hasAdminRole := false
		for _, role := range existingUserWithRoles.Edges.Roles {
			if role.Name == "admin" {
				hasAdminRole = true
				break
			}
		}

		if hasAdminRole {
			logger.Debug("Admin user already exists with admin role, skipping")
			if rerr := tx.Rollback(); rerr != nil {
				logger.Error("failed to rollback transaction", zap.Error(rerr))
			}
			return AdminProvisionAlreadyAdmin, nil
		}

		// User exists but doesn't have admin role - add admin role
		logger.Info("Adding admin role to existing user")

		// Update password if provided and add admin role
		var updateBuilder = tx.User.UpdateOneID(existingUser.ID).
			AddRoleIDs(adminRole.ID).
			SetStatus(user.StatusActive)

		// Update password if it's not the external-auth placeholder
		if existingUser.PasswordHash != auth.ExternalAuthPasswordPlaceholder {
			passwordHash, err := auth.HashPassword(superAdminPassword)
			if err != nil {
				logger.Error("failed to hash admin password", zap.Error(err))
				if rerr := tx.Rollback(); rerr != nil {
					logger.Error("failed to rollback transaction", zap.Error(rerr))
				}
				return AdminProvisionNone, err
			}
			updateBuilder = updateBuilder.SetPasswordHash(passwordHash)
		}

		_, err = updateBuilder.Save(ctx)
		if err != nil {
			logger.Error("failed to update user to admin role", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				logger.Error("failed to rollback transaction", zap.Error(rerr))
			}
			return AdminProvisionNone, err
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			logger.Error("failed to commit admin update transaction", zap.Error(err))
			return AdminProvisionNone, err
		}

		logger.Info("Successfully updated user to admin role")

		return AdminProvisionPromoted, nil
	}

	if !ent.IsNotFound(err) {
		// Real error (not "not found")
		logger.Error("failed to query user by email", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return AdminProvisionNone, err
	}

	// User doesn't exist - create new admin user
	logger.Info("Creating new admin user")

	// Generate username from email (part before @)
	username := "admin"
	if atIndex := strings.Index(superAdminEmail, "@"); atIndex > 0 {
		username = superAdminEmail[:atIndex]
	}

	// Check if username already exists
	usernameExists, err := tx.User.Query().Where(user.UsernameEqualFold(username)).Exist(ctx)
	if err != nil {
		logger.Error("failed to check username existence", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return AdminProvisionNone, err
	}

	// If username exists, append "_admin" suffix
	if usernameExists {
		username = username + "_admin"
	}

	// Hash the password
	passwordHash, err := auth.HashPassword(superAdminPassword)
	if err != nil {
		logger.Error("failed to hash admin password", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return AdminProvisionNone, err
	}

	// Create admin user
	adminUser, err := tx.User.Create().
		SetUsername(username).
		SetEmail(superAdminEmail).
		SetPasswordHash(passwordHash).
		AddRoleIDs(adminRole.ID).
		SetStatus(user.StatusActive).
		SetFirstName("Admin").
		SetLastName("User").
		Save(ctx)
	if err != nil {
		logger.Error("failed to create admin user", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return AdminProvisionNone, err
	}

	// Create user preferences for admin
	_, err = tx.UserPreference.Create().
		SetUserID(adminUser.ID).
		SetModelID(defaultModel.ID).
		SetTheme(userpreference.ThemeDark).
		Save(ctx)
	if err != nil {
		logger.Error("failed to create admin preferences", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return AdminProvisionNone, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		logger.Error("failed to commit admin creation transaction", zap.Error(err))
		return AdminProvisionNone, err
	}

	logger.Info("Successfully created admin user")

	return AdminProvisionCreated, nil
}
