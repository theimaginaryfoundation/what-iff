package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/sql/schema"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
)

// postgresMaxIdentifier is PostgreSQL's identifier limit; longer names are truncated on create,
// so the migration would never find an index under the name it asked for.
const postgresMaxIdentifier = 63

// PrebuildIndexes builds, with CREATE INDEX CONCURRENTLY, every plain index the schema declares on
// a table that already exists but that the database lacks. It runs just before the ent migration,
// which would otherwise build them with a plain CREATE INDEX: that blocks writes to the table for
// the whole build, and on a large hot table (memories, usage_events) it stalls the traffic the
// previous deployment is still serving. Each index gets ent's own name and columns, so the
// migration then finds it present and leaves it alone.
//
// It is best effort. Anything it does not handle is left to the migration, unchanged from before:
// tables that don't exist yet (a fresh database; their indexes are built on empty tables), indexes
// on columns this migration adds, indexes with annotations (partial, typed or op-class indexes),
// and names over PostgreSQL's identifier limit. A failed build is logged and also left to the
// migration. An invalid index, left by a build that was interrupted, is dropped and rebuilt. It
// returns the number of indexes it built, and does nothing outside PostgreSQL.
func PrebuildIndexes(ctx context.Context, db *sql.DB, logger *zap.Logger) (int, error) {
	if databaseType() != "postgres" {
		return 0, nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	built := 0
	for _, table := range migrate.Tables {
		columns, err := liveColumns(ctx, db, table.Name)
		if err != nil {
			return built, err
		}
		if len(columns) == 0 {
			continue // the migration creates the table, with its indexes
		}
		for _, idx := range table.Indexes {
			ddl, ok := concurrentIndexDDL(table.Name, idx, columns)
			if !ok {
				continue
			}
			valid, present, err := indexState(ctx, db, idx.Name)
			if err != nil {
				return built, err
			}
			if present && valid {
				continue
			}
			if present {
				logger.Warn("rebuilding an invalid index left by an interrupted build", zap.String("index", idx.Name))
				if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP INDEX CONCURRENTLY IF EXISTS %q`, idx.Name)); err != nil {
					logger.Warn("could not drop invalid index; leaving it to the migration", zap.String("index", idx.Name), zap.Error(err))
					continue
				}
			}
			logger.Info("building index concurrently ahead of the migration", zap.String("table", table.Name), zap.String("index", idx.Name))
			if _, err := db.ExecContext(ctx, ddl); err != nil {
				logger.Warn("concurrent index build failed; leaving it to the migration", zap.String("index", idx.Name), zap.Error(err))
				continue
			}
			built++
		}
	}
	return built, nil
}

// concurrentIndexDDL returns the CREATE INDEX CONCURRENTLY statement for idx, or false when the
// index is one PrebuildIndexes leaves to the migration (see there). live holds the table's current
// columns.
func concurrentIndexDDL(table string, idx *schema.Index, live map[string]bool) (string, bool) {
	if idx == nil || idx.Annotation != nil || len(idx.Columns) == 0 || len(idx.Name) > postgresMaxIdentifier {
		return "", false
	}
	cols := make([]string, 0, len(idx.Columns))
	for _, c := range idx.Columns {
		if !live[c.Name] {
			return "", false
		}
		cols = append(cols, fmt.Sprintf("%q", c.Name))
	}
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf(`CREATE %sINDEX CONCURRENTLY IF NOT EXISTS %q ON %q (%s)`, unique, idx.Name, table, strings.Join(cols, ", ")), true
}

// liveColumns returns the table's columns, or none when the table does not exist.
func liveColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, fmt.Errorf("list columns of %s: %w", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list columns of %s: %w", table, err)
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// indexState reports whether an index of that name exists in the current schema and is valid.
func indexState(ctx context.Context, db *sql.DB, name string) (valid, present bool, err error) {
	err = db.QueryRowContext(ctx, `
SELECT i.indisvalid
FROM pg_class c
JOIN pg_index i ON i.indexrelid = c.oid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname = $1 AND n.nspname = current_schema()`, name).Scan(&valid)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("look up index %s: %w", name, err)
	}
	return valid, true, nil
}
