package datastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"go.uber.org/zap"
)

func newMoodValidationMockDatastore(t *testing.T) (*Datastore, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	cleanup := func() {
		_ = client.Close()
		_ = db.Close()
	}

	return ds, mock, cleanup
}

func TestSetPersonalityMoods_RejectsNonOwnedMoodIDs(t *testing.T) {
	ds, mock, cleanup := newMoodValidationMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	personalityID := uuid.New()
	moodID := uuid.New()

	mock.ExpectQuery("SELECT .* FROM .*personalities.*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(personalityID.String()))
	mock.ExpectQuery("SELECT .* FROM .*moods.*").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err := ds.SetPersonalityMoods(context.Background(), userID, personalityID, []uuid.UUID{moodID})
	require.True(t, errors.Is(err, ErrMoodNotFound), "expected ErrMoodNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSetChatActiveMood_RejectsMoodOutsideChatPersonality(t *testing.T) {
	ds, mock, cleanup := newMoodValidationMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	modelID := uuid.New()
	moodID := uuid.New()
	now := time.Now()

	mock.ExpectQuery("SELECT .* FROM .*chats.*").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "name", "response_id",
			"checkpoint_summary", "checkpoint_user_message_count", "last_message_time", "last_checkpoint_at",
			"disabled_tools", "is_auto_mood", "chat_model", "chat_personality", "chat_active_mood", "user_chats",
		}).AddRow(
			chatID.String(), now, now, "chat", "", "", 0, now, nil, "[]", true,
			modelID.String(), personalityID.String(), nil, userID.String(),
		))
	mock.ExpectQuery("SELECT .* FROM .*personalities.*").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "name", "system_prompt", "scratchpad",
			"scratchpad_history", "archival_model", "scratchpad_update_prompt",
			"memory_search_prompt", "memory_write_prompt", "auto_pin_memories", "user_personalities",
		}).AddRow(
			personalityID.String(), now, now, "p", "", "", "[]", "", "", "", "", false, userID.String(),
		))
	mock.ExpectQuery("SELECT .* FROM .*moods.*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	err := ds.SetChatActiveMood(context.Background(), userID, chatID, &moodID, nil, nil)
	require.True(t, errors.Is(err, ErrMoodNotFound), "expected ErrMoodNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}
