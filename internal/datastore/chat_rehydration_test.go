package datastore

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// rehydration_state is optional and left unset on every natively created chat,
// so the common case is NULL rather than the documented "". Scanning the column
// straight into a string cannot convert that, which meant the gate logged a
// warning on every message in every native chat — failing open, so nothing
// broke loudly enough to notice.
func TestGetChatRehydrationState_NullReadsAsEmpty(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID, chatID := uuid.New(), uuid.New()
	mock.ExpectQuery("SELECT .*rehydration_state.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "rehydration_state"}).AddRow(chatID, nil))

	state, err := ds.GetChatRehydrationState(context.Background(), userID, chatID)
	require.NoError(t, err, "an unset state is the normal case, not an error")
	require.Equal(t, "", state, `NULL means "no rehydration needed", same as ""`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatRehydrationState_ReadsAStoredValue(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID, chatID := uuid.New(), uuid.New()
	mock.ExpectQuery("SELECT .*rehydration_state.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "rehydration_state"}).AddRow(chatID, "pending"))

	state, err := ds.GetChatRehydrationState(context.Background(), userID, chatID)
	require.NoError(t, err)
	require.Equal(t, "pending", state)
	require.NoError(t, mock.ExpectationsWereMet())
}
