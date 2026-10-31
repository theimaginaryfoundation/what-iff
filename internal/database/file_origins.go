package database

import (
	"context"
	"database/sql"
	"fmt"

	"go.uber.org/zap"
)

// BackfillFileOrigins carries the old agent_authored flag on file_attachments into the origin
// column that replaced it (ADR 0x034 → 0x035): rows the flag marked become origin = 'agent'. It
// runs after the schema migration on every start and is idempotent; once DESTRUCTIVE_MIGRATION has
// dropped the legacy column it does nothing. It returns the number of rows it changed.
func BackfillFileOrigins(ctx context.Context, db *sql.DB, logger *zap.Logger) (int64, error) {
	present, err := columnExists(ctx, db, "file_attachments", "agent_authored")
	if err != nil {
		return 0, fmt.Errorf("check file_attachments.agent_authored: %w", err)
	}
	if !present {
		return 0, nil
	}
	res, err := db.ExecContext(ctx, `UPDATE file_attachments SET origin = 'agent' WHERE agent_authored AND origin <> 'agent'`)
	if err != nil {
		return 0, fmt.Errorf("copy agent_authored into origin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	if n > 0 && logger != nil {
		logger.Info("marked agent-authored files with origin = agent", zap.Int64("rows", n))
	}
	return n, nil
}

// columnExists reports whether table has the named column, on PostgreSQL or SQLite (tests). Both
// names come from this package's constants, never from input.
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var query string
	switch databaseType() {
	case "sqlite3", "sqlite":
		query = fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = '%s'`, table, column)
	default:
		query = fmt.Sprintf(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = '%s' AND column_name = '%s'`, table, column)
	}
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
