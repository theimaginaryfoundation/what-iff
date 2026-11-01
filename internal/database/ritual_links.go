package database

import (
	"context"
	"database/sql"
	"fmt"

	"go.uber.org/zap"
)

// ritualLinkBackfill describes one legacy one-to-many link column on `rituals` and the
// many-to-many join table that replaced it (ADR 0x035 §11).
//
// Every name here is interpolated into SQL. They are constants of this package and must never
// come from input.
type ritualLinkBackfill struct {
	// column is the FK column ent created on `rituals` while Mood.rituals / AgentJob.rituals had
	// no inverse edge. The join table that replaced it has the same name.
	column string
	// ownerTable and ownerColumn name the mode or job a link points at.
	ownerTable  string
	ownerColumn string
}

var ritualLinkBackfills = []ritualLinkBackfill{
	{column: "mood_rituals", ownerTable: "moods", ownerColumn: "mood_id"},
	{column: "agent_job_rituals", ownerTable: "agent_jobs", ownerColumn: "agent_job_id"},
}

// BackfillRitualLinks moves the mode and job links that the old one-to-many columns on `rituals`
// still hold into the `mood_rituals` and `agent_job_rituals` join tables. It runs after the
// schema migration on every start; logger may be nil.
//
// For each column, in one transaction, it copies the links whose mode or job still exists and
// then clears the column. Clearing is what makes a second run a no-op: a non-destructive
// migration keeps the column but drops its foreign key (and with it ON DELETE SET NULL), so a
// value left behind would either point at a deleted mode (and fail the join table's foreign
// key) or bring back a link the user removed since. A column that no longer exists is skipped.
// It returns the number of links copied.
func BackfillRitualLinks(ctx context.Context, db *sql.DB, logger *zap.Logger) (int64, error) {
	var copied int64
	for _, b := range ritualLinkBackfills {
		present, err := columnExists(ctx, db, "rituals", b.column)
		if err != nil {
			return copied, fmt.Errorf("check rituals.%s: %w", b.column, err)
		}
		if !present {
			continue
		}
		n, err := moveRitualLinks(ctx, db, b)
		if err != nil {
			return copied, err
		}
		copied += n
		if n > 0 && logger != nil {
			logger.Info("copied skill links into join table", zap.String("table", b.column), zap.Int64("rows", n))
		}
	}
	return copied, nil
}

func moveRitualLinks(ctx context.Context, db *sql.DB, b ritualLinkBackfill) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin moving rituals.%s: %w", b.column, err)
	}
	defer func() { _ = tx.Rollback() }()

	// The legacy column and the join table share a name; qualified with "rituals." it is the
	// column. The JOIN drops links to a mode or job deleted after the foreign key went away.
	res, err := tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %[1]s (%[3]s, ritual_id)
		 SELECT rituals.%[1]s, rituals.id FROM rituals JOIN %[2]s ON %[2]s.id = rituals.%[1]s
		 WHERE rituals.%[1]s IS NOT NULL
		 ON CONFLICT DO NOTHING`,
		b.column, b.ownerTable, b.ownerColumn))
	if err != nil {
		return 0, fmt.Errorf("copy rituals.%s into %s: %w", b.column, b.column, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		n = 0
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE rituals SET %[1]s = NULL WHERE %[1]s IS NOT NULL`, b.column)); err != nil {
		return 0, fmt.Errorf("clear rituals.%s: %w", b.column, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit moving rituals.%s: %w", b.column, err)
	}
	return n, nil
}

// columnExists reports whether table has the named column, on PostgreSQL or SQLite (tests).
// databaseType() is DB_TYPE, defaulting to postgres. Both names are package constants, never
// input.
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var query string
	switch databaseType() {
	case "sqlite3", "sqlite":
		query = fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = '%s'`, table, column)
	default:
		query = fmt.Sprintf(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = '%s' AND column_name = '%s'`, table, column)
	}
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
