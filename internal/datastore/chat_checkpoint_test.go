package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"go.uber.org/zap"
)

func newMockDatastore(t *testing.T) (*Datastore, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	ds, cleanup := newDatastoreOverMockDB(t, db)
	return ds, mock, cleanup
}

// newDatastoreOverMockDB wires a Datastore onto an already-created mock DB. Split out so
// helpers that need to configure sqlmock themselves — see newCapturingMockDatastore in
// userpreferences_test.go — share this wiring rather than copying it and drifting from it
// if the driver, logger, or encryption key ever change.
func newDatastoreOverMockDB(t *testing.T, db *sql.DB) (*Datastore, func()) {
	t.Helper()

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	cleanup := func() {
		_ = client.Close()
		_ = db.Close()
	}

	return ds, cleanup
}

func TestUpdateChatCheckpointStateAndClearResponseID_Success(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*chats.*").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := ds.UpdateChatCheckpointStateAndClearResponseID(context.Background(), userID, chatID, "checkpoint-summary", 42)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateChatCheckpointStateAndClearResponseID_NotFoundOrUnauthorized(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*chats.*").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := ds.UpdateChatCheckpointStateAndClearResponseID(context.Background(), userID, chatID, "checkpoint-summary", 42)
	require.True(t, errors.Is(err, ErrChatNotFound), "expected ErrChatNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateChatCheckpointStateAndClearResponseID_UpdateFails_Rollback(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*chats.*").
		WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	err := ds.UpdateChatCheckpointStateAndClearResponseID(context.Background(), userID, chatID, "checkpoint-summary", 42)
	require.Error(t, err)
	require.ErrorContains(t, err, "db error")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestToChatModel_PrefersSummaryMemoryOverLegacyCheckpointSummary(t *testing.T) {
	entChat := &ent.Chat{
		ID:                uuid.New(),
		Name:              "Checkpoint Chat",
		CheckpointSummary: "legacy summary",
		Edges: ent.ChatEdges{
			Memories: []*ent.Memory{
				{
					ID:      uuid.New(),
					Content: "summary memory",
					Scope:   entmemory.ScopeSummary,
				},
			},
		},
	}

	chat := toChatModel(entChat)

	require.NotNil(t, chat)
	require.Equal(t, "summary memory", chat.CheckpointSummary)
}

func TestToChatModel_FallsBackToLegacyCheckpointSummary(t *testing.T) {
	entChat := &ent.Chat{
		ID:                uuid.New(),
		Name:              "Checkpoint Chat",
		CheckpointSummary: "legacy summary",
	}

	chat := toChatModel(entChat)

	require.NotNil(t, chat)
	require.Equal(t, "legacy summary", chat.CheckpointSummary)
}
