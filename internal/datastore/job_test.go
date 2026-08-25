package datastore

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestAppendJobDraftDeltas_NoOpWhenEmpty(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	err := ds.AppendJobDraftDeltas(context.Background(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAppendJobDraftDeltas_UsesSingleScopedUpdate(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*user_jobs.*").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := ds.AppendJobDraftDeltas(
		context.Background(),
		uuid.New(),
		uuid.New(),
		[]string{"hello", " world"},
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAppendJobDraftDeltas_ReturnsUpdateError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*").
		WillReturnError(errors.New("update failed"))

	err := ds.AppendJobDraftDeltas(context.Background(), uuid.New(), uuid.New(), []string{"chunk"})
	require.Error(t, err)
	require.ErrorContains(t, err, "update failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFinalizeFailedChatJobWithPartial_PersistsDraftAndFailure(t *testing.T) {
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()
	createPartialJobTestTables(t, ds)
	ctx := context.Background()

	user, err := ds.dbClient.User.Create().
		SetUsername("partial-user").
		SetEmail("partial@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	chat, err := ds.dbClient.Chat.Create().SetName("Partial reply").SetOwnerID(user.ID).Save(ctx)
	require.NoError(t, err)
	created, err := ds.CreateJob(ctx, user.ID, models.Job{
		JobType:   "chat_message",
		Reference: uuid.NewString(),
		Status:    models.JobStatusProcessing,
	})
	require.NoError(t, err)
	require.NoError(t, ds.AppendJobDraftDeltas(ctx, user.ID, created.ID, []string{"A partial ", "answer."}))

	updated, resultID, err := ds.FinalizeFailedChatJobWithPartial(
		ctx, user.ID, created.ID, chat.ID, "claude-sonnet", "Scribe", "provider rejected a later tool step", nil,
	)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, updated.Status)
	require.Equal(t, "provider rejected a later tool step", updated.Error)
	require.Empty(t, updated.DraftDeltas)
	require.NotNil(t, resultID)
	require.Equal(t, resultID, updated.ResultID)

	msg, err := ds.dbClient.ChatMessage.Query().Where(chatmessage.ID(*resultID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "A partial answer.", msg.Message)
	require.Equal(t, chatmessage.OriginAssistant, msg.Origin)
	require.Equal(t, "claude-sonnet", msg.GenerationModel)
	require.Equal(t, "Scribe", msg.GenerationPersonality)
}

// createPartialJobTestTables hand-writes the tables this test needs rather than
// running Ent's migration.
//
// That hand-writing is why this test broke on main once already: one PR added
// chat_messages.context_breakdown to the Ent schema while another added the
// CREATE TABLE below without it; they merged within hours of each other, and
// every write to chat_messages here failed with "no such column". Each PR was
// green against a main that did not yet contain the other.
//
// So: a column added to ent/schema/chatmessage.go has to be added here too.
// Nothing enforces that — the compiler cannot see inside a SQL string — which is
// the standing hazard of a hand-written schema in a test.
func createPartialJobTestTables(t *testing.T, ds *Datastore) {
	t.Helper()
	for _, statement := range []string{
		`ALTER TABLE chats ADD COLUMN source text`,
		`ALTER TABLE chats ADD COLUMN import_hash text`,
		`ALTER TABLE chats ADD COLUMN rehydration_state text`,
		`CREATE TABLE jobs (
			id uuid PRIMARY KEY,
			job_type text NOT NULL,
			reference text NOT NULL,
			status text NOT NULL,
			error text,
			result_id uuid,
			draft_deltas json,
			progress text,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_jobs uuid NOT NULL
		)`,
		`CREATE TABLE chat_messages (
			id uuid PRIMARY KEY,
			message text NOT NULL,
			origin text NOT NULL,
			read_status text NOT NULL,
			response_id text,
			sent_at datetime NOT NULL,
			tokens integer,
			generation_model text,
			generation_personality text,
			generation_expression_reasoning text,
			last_error_message text,
			checkpoint_completed_at datetime,
			context_breakdown json,
			chat_messages uuid NOT NULL,
			chat_message_generation_mood uuid,
			chat_message_generation_expression uuid
		)`,
	} {
		_, err := ds.sqlDB.Exec(statement)
		require.NoError(t, err)
	}
}
