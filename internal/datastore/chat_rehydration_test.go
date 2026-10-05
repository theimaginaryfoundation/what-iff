package datastore

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClaimChatRehydration_ClaimsOnlyAQualifyingThread(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	// The conditions live in the UPDATE itself, so the claim is atomic: unarchived, imported, no
	// summary yet, and not pending/processing/ready.
	mock.ExpectExec("UPDATE .chats. SET .updated_at. = \\?, .rehydration_state. = \\? WHERE "+
		".*NOT .chats.\\..archived..*"+
		".chats.\\..source. IS NOT NULL.*.chats.\\..source. <> \\?.*"+
		".chats.\\..checkpoint_summary. IS NULL OR .chats.\\..checkpoint_summary. = \\?.*"+
		".chats.\\..rehydration_state. IS NULL OR .chats.\\..rehydration_state. IN \\(\\?, \\?\\)").
		WithArgs(sqlmock.AnyArg(), "pending", sqlmock.AnyArg(), sqlmock.AnyArg(), "", "", "", "failed").
		WillReturnResult(sqlmock.NewResult(0, 1))

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimChatRehydration_NotClaimedWhenNothingMatches(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE .*chats.*`).WillReturnResult(sqlmock.NewResult(0, 0))

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimChatRehydration_ReturnsUpdateError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE .*chats.*`).WillReturnError(context.DeadlineExceeded)

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
	require.False(t, claimed)
}
