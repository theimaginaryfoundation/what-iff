package database

import (
	"context"
	"database/sql"
	"fmt"

	"go.uber.org/zap"
)

// ritualLinkBackfill describes one legacy one-to-many link column on `rituals` and the
// many-to-many join table that replaced it (ADR 0x035 §11).
type ritualLinkBackfill struct {
	// legacyColumn is the FK column ent created on `rituals` while Mood.rituals / AgentJob.rituals
	// had no inverse edge. It shares its name with the join table.
	legacyColumn string
	joinTable    string
	ownerColumn  string
}

var ritualLinkBackfills = []ritualLinkBackfill{
	{legacyColumn: "mood_rituals", joinTable: "mood_rituals", ownerColumn: "mood_id"},
	{legacyColumn: "agent_job_rituals", joinTable: "agent_job_rituals", ownerColumn: "agent_job_id"},
}

// BackfillRitualLinks copies the mode and job links that the old one-to-many columns on
// `rituals` still hold into the `mood_rituals` and `agent_job_rituals` join tables. It runs
// after the schema migration on every start and is idempotent: rows already present are
// skipped, and a column that no longer exists (dropped by DESTRUCTIVE_MIGRATION) is ignored.
// It returns the number of links copied.
func BackfillRitualLinks(ctx context.Context, db *sql.DB, logger *zap.Logger) (int64, error) {
	var copied int64
	for _, b := range ritualLinkBackfills {
		present, err := ritualsColumnExists(ctx, db, b.legacyColumn)
		if err != nil {
			return copied, fmt.Errorf("check rituals.%s: %w", b.legacyColumn, err)
		}
		if !present {
			continue
		}
		// The legacy column and the join table share a name; inside "FROM rituals" the bare
		// identifier resolves to the column.
		res, err := db.ExecContext(ctx, fmt.Sprintf(
			`INSERT INTO %s (%s, ritual_id) SELECT rituals.%s, rituals.id FROM rituals WHERE rituals.%s IS NOT NULL ON CONFLICT DO NOTHING`,
			b.joinTable, b.ownerColumn, b.legacyColumn, b.legacyColumn))
		if err != nil {
			return copied, fmt.Errorf("copy rituals.%s into %s: %w", b.legacyColumn, b.joinTable, err)
		}
		n, err := res.RowsAffected()
		if err == nil && n > 0 {
			copied += n
			if logger != nil {
				logger.Info("copied skill links into join table",
					zap.String("table", b.joinTable), zap.Int64("rows", n))
			}
		}
	}
	return copied, nil
}

// ritualsColumnExists reports whether `rituals` has the named column.
func ritualsColumnExists(ctx context.Context, db *sql.DB, column string) (bool, error) {
	return columnExists(ctx, db, "rituals", column)
}
