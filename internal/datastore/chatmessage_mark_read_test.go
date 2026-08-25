package datastore

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMarkChatMessagesRead_Success(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
	mock.ExpectExec("UPDATE .*chat_messages.*").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	updatedCount, err := ds.MarkChatMessagesRead(context.Background(), userID, chatID)
	require.NoError(t, err)
	require.Equal(t, 2, updatedCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkChatMessagesRead_ChatNotFound(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	updatedCount, err := ds.MarkChatMessagesRead(context.Background(), userID, chatID)
	require.True(t, errors.Is(err, ErrChatNotFound), "expected ErrChatNotFound, got %v", err)
	require.Equal(t, 0, updatedCount)
	require.NoError(t, mock.ExpectationsWereMet())
}
