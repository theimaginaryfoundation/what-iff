package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

const modelSubscriptionTierUltra = "ultra"

var (
	// subscription_tier IN ('low', 'medium', 'high')
	subscriptionTierInCheckPattern = regexp.MustCompile(`(?i)subscription_tier\s+IN\s*\(([^)]+)\)`)
)

// MigrateModelSubscriptionTierUltra attempts to ensure models.subscription_tier accepts "ultra".
// Ent schema.Create does not alter existing PostgreSQL enum types or CHECK constraints.
//
// This is best-effort: failures are logged but never block API startup. App-layer validation
// still enforces tier values when the DB constraint cannot be widened automatically.
func MigrateModelSubscriptionTierUltra(ctx context.Context, sqlDB *sql.DB, logger *zap.Logger) {
	if sqlDB == nil || os.Getenv("DB_TYPE") != "postgres" {
		return
	}

	var typName, typCategory string
	err := sqlDB.QueryRowContext(ctx, `
		SELECT pg_type.typname, pg_type.typtype::text
		FROM pg_attribute
		JOIN pg_class ON pg_attribute.attrelid = pg_class.oid
		JOIN pg_type ON pg_attribute.atttypid = pg_type.oid
		JOIN pg_namespace ON pg_class.relnamespace = pg_namespace.oid
		WHERE pg_namespace.nspname = current_schema()
		  AND pg_class.relname = 'models'
		  AND pg_attribute.attname = 'subscription_tier'
		  AND pg_attribute.attnum > 0
		  AND NOT pg_attribute.attisdropped
	`).Scan(&typName, &typCategory)
	if err == sql.ErrNoRows {
		logger.Debug("models.subscription_tier column not found; skipping ultra migration")
		return
	}
	if err != nil {
		logger.Warn("ultra tier migration: could not inspect models.subscription_tier column; relying on app validation",
			zap.Error(err))
		return
	}

	switch typCategory {
	case "e":
		migratePostgresEnumSubscriptionTier(ctx, sqlDB, logger, typName)
	case "b":
		// Ent stores field.Enum as varchar + CHECK on Postgres (not native enum types).
		switch typName {
		case "varchar", "text", "bpchar":
			migrateVarcharSubscriptionTierCheck(ctx, sqlDB, logger)
		default:
			logger.Debug("models.subscription_tier uses unsupported base type; skipping ultra migration",
				zap.String("typname", typName))
		}
	default:
		logger.Debug("models.subscription_tier uses unsupported postgres type category; skipping ultra migration",
			zap.String("typname", typName),
			zap.String("typtype", typCategory))
	}
}

func migratePostgresEnumSubscriptionTier(ctx context.Context, sqlDB *sql.DB, logger *zap.Logger, enumType string) {
	var exists bool
	err := sqlDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_enum e
			JOIN pg_type t ON e.enumtypid = t.oid
			WHERE t.typname = $1
			  AND e.enumlabel = $2
		)
	`, enumType, modelSubscriptionTierUltra).Scan(&exists)
	if err != nil {
		logger.Warn("ultra tier migration: could not check postgres enum for ultra",
			zap.String("enum_type", enumType),
			zap.Error(err))
		return
	}
	if exists {
		logger.Debug("models.subscription_tier already includes ultra", zap.String("enum_type", enumType))
		return
	}

	if _, err := sqlDB.ExecContext(ctx,
		fmt.Sprintf(`ALTER TYPE %q ADD VALUE IF NOT EXISTS %q`, enumType, modelSubscriptionTierUltra),
	); err != nil {
		logger.Warn("ultra tier migration: could not add ultra to postgres enum; ultra tier may fail at DB layer",
			zap.String("enum_type", enumType),
			zap.Error(err))
		return
	}

	logger.Info("Added ultra value to models.subscription_tier enum", zap.String("enum_type", enumType))
}

func migrateVarcharSubscriptionTierCheck(ctx context.Context, sqlDB *sql.DB, logger *zap.Logger) {
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT c.conname, pg_get_constraintdef(c.oid, true)
		FROM pg_constraint c
		JOIN pg_class rel ON c.conrelid = rel.oid
		JOIN pg_namespace n ON rel.relnamespace = n.oid
		WHERE n.nspname = current_schema()
		  AND rel.relname = 'models'
		  AND c.contype = 'c'
	`)
	if err != nil {
		logger.Warn("ultra tier migration: could not list models check constraints; relying on app validation",
			zap.Error(err))
		return
	}
	defer rows.Close()

	var updated bool
	var skippedShape int
	for rows.Next() {
		var conName, conDef string
		if err := rows.Scan(&conName, &conDef); err != nil {
			logger.Warn("ultra tier migration: could not read check constraint row; skipping remainder",
				zap.Error(err))
			return
		}
		if !strings.Contains(strings.ToLower(conDef), "subscription_tier") {
			continue
		}
		newDef, ok := subscriptionTierCheckWithUltra(conDef)
		if !ok {
			skippedShape++
			logger.Debug("subscription_tier check constraint shape not recognized; skipping",
				zap.String("constraint", conName),
				zap.String("definition", conDef))
			continue
		}
		if newDef == conDef {
			logger.Debug("models.subscription_tier check constraint already includes ultra",
				zap.String("constraint", conName))
			continue
		}

		if _, err := sqlDB.ExecContext(ctx,
			fmt.Sprintf(`ALTER TABLE models DROP CONSTRAINT %q`, conName),
		); err != nil {
			logger.Warn("ultra tier migration: could not drop check constraint before widening",
				zap.String("constraint", conName),
				zap.Error(err))
			continue
		}
		if _, err := sqlDB.ExecContext(ctx,
			fmt.Sprintf(`ALTER TABLE models ADD CONSTRAINT %q CHECK (%s)`, conName, newDef),
		); err != nil {
			logger.Error("ultra tier migration: dropped check constraint but failed to recreate with ultra; manual DDL may be required",
				zap.String("constraint", conName),
				zap.String("intended_definition", newDef),
				zap.Error(err))
			continue
		}
		logger.Info("Updated models.subscription_tier check constraint to include ultra",
			zap.String("constraint", conName))
		updated = true
	}
	if err := rows.Err(); err != nil {
		logger.Warn("ultra tier migration: error iterating check constraints",
			zap.Error(err))
		return
	}

	if skippedShape > 0 {
		logger.Warn("ultra tier migration: skipped unrecognized subscription_tier CHECK constraint(s); ultra may fail at DB layer",
			zap.Int("skipped", skippedShape))
	}
	if !updated {
		logger.Debug("no models.subscription_tier varchar CHECK constraint updated; ultra writes rely on app validation")
	}
}

func subscriptionTierCheckWithUltra(def string) (string, bool) {
	if strings.Contains(strings.ToLower(def), "'ultra'") {
		return def, true
	}

	if m := subscriptionTierInCheckPattern.FindStringSubmatch(def); len(m) >= 2 {
		valuesPart := strings.TrimSpace(m[1])
		expanded := valuesPart
		if expanded != "" && !strings.HasSuffix(expanded, ",") {
			expanded += ", "
		}
		expanded += "'ultra'"
		return subscriptionTierInCheckPattern.ReplaceAllString(def, "subscription_tier IN ("+expanded+")"), true
	}

	if expanded, ok := expandEntVarcharEnumArrayCheck(def); ok {
		return expanded, true
	}

	return "", false
}

// expandEntVarcharEnumArrayCheck handles Ent's default Postgres enum storage:
// ((subscription_tier)::text = ANY ((ARRAY['low'::character varying, ...])::text[]))
func expandEntVarcharEnumArrayCheck(def string) (string, bool) {
	lower := strings.ToLower(def)
	if !strings.Contains(lower, "subscription_tier") || !strings.Contains(lower, "array[") {
		return "", false
	}
	if strings.Contains(lower, "'ultra'") {
		return def, true
	}
	const marker = "])::text[]"
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return "", false
	}
	return def[:idx] + ", 'ultra'::character varying" + def[idx:], true
}
