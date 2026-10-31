package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"
)

// newRitualLinksDB builds the three tables the backfill touches, with or without the legacy
// one-to-many columns on rituals.
func newRitualLinksDB(t *testing.T, legacyColumns bool) *sql.DB {
	t.Helper()
	t.Setenv("DB_TYPE", "sqlite3")
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ritualsDDL := `CREATE TABLE rituals (id TEXT PRIMARY KEY`
	if legacyColumns {
		ritualsDDL += `, mood_rituals TEXT NULL, agent_job_rituals TEXT NULL`
	}
	ritualsDDL += `)`
	for _, ddl := range []string{
		ritualsDDL,
		`CREATE TABLE mood_rituals (mood_id TEXT NOT NULL, ritual_id TEXT NOT NULL, PRIMARY KEY (mood_id, ritual_id))`,
		`CREATE TABLE agent_job_rituals (agent_job_id TEXT NOT NULL, ritual_id TEXT NOT NULL, PRIMARY KEY (agent_job_id, ritual_id))`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
	return db
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

func TestBackfillRitualLinks_CopiesLegacyColumnsOnce(t *testing.T) {
	db := newRitualLinksDB(t, true)
	ctx := context.Background()

	moodID, jobID := uuid.NewString(), uuid.NewString()
	onMood, onJob, onBoth, unlinked := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, row := range []struct{ id, mood, job any }{
		{onMood, moodID, nil},
		{onJob, nil, jobID},
		{onBoth, moodID, jobID},
		{unlinked, nil, nil},
	} {
		_, err := db.Exec(`INSERT INTO rituals (id, mood_rituals, agent_job_rituals) VALUES (?, ?, ?)`, row.id, row.mood, row.job)
		require.NoError(t, err)
	}
	// A link already in the join table (a mode edited after the schema changed) is kept, not duplicated.
	_, err := db.Exec(`INSERT INTO mood_rituals (mood_id, ritual_id) VALUES (?, ?)`, moodID, onBoth)
	require.NoError(t, err)

	copied, err := BackfillRitualLinks(ctx, db, zap.NewNop())
	require.NoError(t, err)
	require.EqualValues(t, 3, copied, "onMood, onJob and onBoth's job link; onBoth's mood link already existed")
	require.Equal(t, 2, countRows(t, db, "mood_rituals"))
	require.Equal(t, 2, countRows(t, db, "agent_job_rituals"))

	copied, err = BackfillRitualLinks(ctx, db, zap.NewNop())
	require.NoError(t, err)
	require.Zero(t, copied, "second run is a no-op")
	require.Equal(t, 2, countRows(t, db, "mood_rituals"))
	require.Equal(t, 2, countRows(t, db, "agent_job_rituals"))
}

func TestBackfillRitualLinks_SkipsWhenColumnsAreGone(t *testing.T) {
	db := newRitualLinksDB(t, false)
	_, err := db.Exec(`INSERT INTO rituals (id) VALUES (?)`, uuid.NewString())
	require.NoError(t, err)

	copied, err := BackfillRitualLinks(context.Background(), db, nil)
	require.NoError(t, err)
	require.Zero(t, copied)
	require.Equal(t, 0, countRows(t, db, "mood_rituals"))
}
