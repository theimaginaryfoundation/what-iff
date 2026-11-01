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

// newRitualLinksDB builds the tables the backfill touches the way a non-destructive Postgres
// migration leaves them: the join tables carry foreign keys to their owner and to rituals, and
// the legacy columns (when present) are plain columns with no foreign key, so deleting a mode
// does not clear them.
func newRitualLinksDB(t *testing.T, legacyColumns bool) *sql.DB {
	t.Helper()
	t.Setenv("DB_TYPE", "sqlite3")
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=1")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ritualsDDL := `CREATE TABLE rituals (id TEXT PRIMARY KEY`
	if legacyColumns {
		ritualsDDL += `, mood_rituals TEXT NULL, agent_job_rituals TEXT NULL`
	}
	ritualsDDL += `)`
	for _, ddl := range []string{
		`CREATE TABLE moods (id TEXT PRIMARY KEY)`,
		`CREATE TABLE agent_jobs (id TEXT PRIMARY KEY)`,
		ritualsDDL,
		`CREATE TABLE mood_rituals (mood_id TEXT NOT NULL REFERENCES moods(id) ON DELETE CASCADE,
			ritual_id TEXT NOT NULL REFERENCES rituals(id) ON DELETE CASCADE, PRIMARY KEY (mood_id, ritual_id))`,
		`CREATE TABLE agent_job_rituals (agent_job_id TEXT NOT NULL REFERENCES agent_jobs(id) ON DELETE CASCADE,
			ritual_id TEXT NOT NULL REFERENCES rituals(id) ON DELETE CASCADE, PRIMARY KEY (agent_job_id, ritual_id))`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
	return db
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestBackfillRitualLinks_MovesLegacyColumnsOnce(t *testing.T) {
	db := newRitualLinksDB(t, true)
	ctx := context.Background()

	moodID, jobID := uuid.NewString(), uuid.NewString()
	exec(t, db, `INSERT INTO moods (id) VALUES (?)`, moodID)
	exec(t, db, `INSERT INTO agent_jobs (id) VALUES (?)`, jobID)
	onMood, onJob, onBoth, unlinked := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, row := range []struct{ id, mood, job any }{
		{onMood, moodID, nil},
		{onJob, nil, jobID},
		{onBoth, moodID, jobID},
		{unlinked, nil, nil},
	} {
		exec(t, db, `INSERT INTO rituals (id, mood_rituals, agent_job_rituals) VALUES (?, ?, ?)`, row.id, row.mood, row.job)
	}
	// A link already in the join table (a mode edited after the schema changed) is kept, not duplicated.
	exec(t, db, `INSERT INTO mood_rituals (mood_id, ritual_id) VALUES (?, ?)`, moodID, onBoth)

	copied, err := BackfillRitualLinks(ctx, db, zap.NewNop())
	require.NoError(t, err)
	require.EqualValues(t, 3, copied, "onMood, onJob and onBoth's job link; onBoth's mood link already existed")
	require.Equal(t, 2, countRows(t, db, `SELECT COUNT(*) FROM mood_rituals`))
	require.Equal(t, 2, countRows(t, db, `SELECT COUNT(*) FROM agent_job_rituals`))
	require.Zero(t, countRows(t, db, `SELECT COUNT(*) FROM rituals WHERE mood_rituals IS NOT NULL OR agent_job_rituals IS NOT NULL`),
		"the legacy columns are cleared once copied")

	copied, err = BackfillRitualLinks(ctx, db, zap.NewNop())
	require.NoError(t, err)
	require.Zero(t, copied, "second run is a no-op")
}

func TestBackfillRitualLinks_RemovedLinkStaysRemovedAcrossRestarts(t *testing.T) {
	db := newRitualLinksDB(t, true)
	ctx := context.Background()
	moodID, ritualID := uuid.NewString(), uuid.NewString()
	exec(t, db, `INSERT INTO moods (id) VALUES (?)`, moodID)
	exec(t, db, `INSERT INTO rituals (id, mood_rituals) VALUES (?, ?)`, ritualID, moodID)

	_, err := BackfillRitualLinks(ctx, db, nil)
	require.NoError(t, err)
	// The user detaches the skill from the mode.
	exec(t, db, `DELETE FROM mood_rituals WHERE mood_id = ? AND ritual_id = ?`, moodID, ritualID)

	copied, err := BackfillRitualLinks(ctx, db, nil) // next boot
	require.NoError(t, err)
	require.Zero(t, copied)
	require.Zero(t, countRows(t, db, `SELECT COUNT(*) FROM mood_rituals`), "the detached link must not come back")
}

func TestBackfillRitualLinks_SkipsLinksToDeletedOwners(t *testing.T) {
	db := newRitualLinksDB(t, true)
	ctx := context.Background()
	liveMood, deletedMood, deletedJob := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(t, db, `INSERT INTO moods (id) VALUES (?)`, liveMood)
	onLive, onDeletedMood, onDeletedJob := uuid.NewString(), uuid.NewString(), uuid.NewString()
	// The legacy columns have no foreign key any more, so these ids outlived their mode and job.
	exec(t, db, `INSERT INTO rituals (id, mood_rituals) VALUES (?, ?)`, onLive, liveMood)
	exec(t, db, `INSERT INTO rituals (id, mood_rituals) VALUES (?, ?)`, onDeletedMood, deletedMood)
	exec(t, db, `INSERT INTO rituals (id, agent_job_rituals) VALUES (?, ?)`, onDeletedJob, deletedJob)

	copied, err := BackfillRitualLinks(ctx, db, nil)
	require.NoError(t, err, "a stale id must not fail the join table's foreign key")
	require.EqualValues(t, 1, copied)
	require.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM mood_rituals WHERE mood_id = ?`, liveMood))
	require.Zero(t, countRows(t, db, `SELECT COUNT(*) FROM rituals WHERE mood_rituals IS NOT NULL OR agent_job_rituals IS NOT NULL`),
		"stale ids are cleared too")
}

func TestBackfillRitualLinks_SkipsWhenColumnsAreGone(t *testing.T) {
	db := newRitualLinksDB(t, false)
	exec(t, db, `INSERT INTO rituals (id) VALUES (?)`, uuid.NewString())

	copied, err := BackfillRitualLinks(context.Background(), db, nil)
	require.NoError(t, err)
	require.Zero(t, copied)
	require.Zero(t, countRows(t, db, `SELECT COUNT(*) FROM mood_rituals`))
}
