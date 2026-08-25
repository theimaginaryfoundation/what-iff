package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/ent"

	"go.uber.org/zap"
)

// MigrateRoleToJoinTable ensures the roles table and user_roles join table exist.
// This must be called BEFORE Ent schema migration to avoid constraint violations.
func MigrateRoleToJoinTable(ctx context.Context, db *ent.Client, sqlDB *sql.DB, logger *zap.Logger) error {
	// Check if migration is already complete by checking if user_roles table exists
	var tableExists bool
	err := sqlDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'user_roles'
		)
	`).Scan(&tableExists)

	if err != nil && err != sql.ErrNoRows {
		logger.Warn("Failed to check if user_roles table exists, proceeding with migration", zap.Error(err))
	}

	// If table exists, check if it has data (migration might be complete)
	if tableExists {
		var rowCount int
		err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles`).Scan(&rowCount)
		if err == nil && rowCount > 0 {
			logger.Info("User roles join table already exists with data, skipping migration")
			return nil
		}
	}

	// Perform migration
	return migrateRoleData(ctx, db, sqlDB, logger)
}

// migrateRoleData creates the roles table and user_roles join table, and seeds default roles
func migrateRoleData(ctx context.Context, db *ent.Client, sqlDB *sql.DB, logger *zap.Logger) error {
	// Step 1: Create roles table if it doesn't exist
	_, err := sqlDB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS roles (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name varchar(100) NOT NULL UNIQUE,
			description varchar(500),
			created_at timestamp NOT NULL DEFAULT NOW(),
			updated_at timestamp NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create roles table: %w", err)
	}
	logger.Info("Created roles table (if it didn't exist)")

	// Step 2: Seed default roles if they don't exist
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO roles (id, name, description, created_at, updated_at)
		SELECT
			gen_random_uuid(),
			'user',
			'Default user role with standard permissions',
			NOW(),
			NOW()
		WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'user')
	`)
	if err != nil {
		return fmt.Errorf("failed to seed user role: %w", err)
	}

	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO roles (id, name, description, created_at, updated_at)
		SELECT
			gen_random_uuid(),
			'admin',
			'Administrator role with elevated permissions',
			NOW(),
			NOW()
		WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'admin')
	`)
	if err != nil {
		return fmt.Errorf("failed to seed admin role: %w", err)
	}
	logger.Info("Seeded default roles (user and admin)")

	// Step 3: Get role IDs
	var userRoleID, adminRoleID string
	err = sqlDB.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = 'user' LIMIT 1`).Scan(&userRoleID)
	if err != nil {
		return fmt.Errorf("failed to get user role ID: %w", err)
	}

	err = sqlDB.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = 'admin' LIMIT 1`).Scan(&adminRoleID)
	if err != nil {
		return fmt.Errorf("failed to get admin role ID: %w", err)
	}

	// Step 4: Check if we need to assign roles by checking if any users exist
	var userCount int
	err = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount)
	if err != nil {
		// If users table doesn't exist, that's okay - Ent will create it
		userCount = 0
	}

	// If no users exist, migration is not needed (Ent will create the join table)
	if userCount == 0 {
		logger.Info("No users to assign roles, skipping role assignment")
		return nil
	}

	// Step 5: Start transaction for atomic migration
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Step 6: Create user_roles join table if it doesn't exist
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_roles (
			user_id uuid NOT NULL,
			role_id uuid NOT NULL,
			PRIMARY KEY (user_id, role_id),
			CONSTRAINT user_roles_user_id_fk FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			CONSTRAINT user_roles_role_id_fk FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return fmt.Errorf("failed to create user_roles join table: %w", err)
	}

	// Step 7: Create indexes for better query performance
	_, err = tx.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);
		CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles(role_id);
	`)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	// Step 8: Ensure all existing users have at least the default 'user' role
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT
			u.id,
			$1::uuid
		FROM users u
		WHERE NOT EXISTS (
			SELECT 1 FROM user_roles ur
			WHERE ur.user_id = u.id
		)
	`, userRoleID)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return fmt.Errorf("failed to assign default role to users: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	logger.Info("Successfully set up roles and user_roles join table",
		zap.Int("users_processed", userCount))

	return nil
}
