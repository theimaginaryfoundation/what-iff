package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClaimChatCheckpoint_ClaimsAFreeOrStaleChat(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	// One conditional UPDATE decides: no claim yet, or one older than the stale bound.
	mock.ExpectExec("UPDATE .chats. SET .updated_at. = \\?, .checkpoint_started_at. = \\? WHERE "+
		".*.chats.\\..checkpoint_started_at. IS NULL OR .chats.\\..checkpoint_started_at. < \\?").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	claimedAt, claimed, err := ds.ClaimChatCheckpoint(context.Background(), uuid.New(), uuid.New(), 10*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.WithinDuration(t, time.Now(), claimedAt, time.Minute)
	require.Zero(t, claimedAt.Nanosecond()%1000, "microsecond precision, so release matches the stored value")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimChatCheckpoint_NotClaimedWhileAnotherIsRunning(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 0))

	_, claimed, err := ds.ClaimChatCheckpoint(context.Background(), uuid.New(), uuid.New(), 10*time.Minute)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimChatCheckpoint_ReturnsUpdateError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*").WillReturnError(context.DeadlineExceeded)

	_, claimed, err := ds.ClaimChatCheckpoint(context.Background(), uuid.New(), uuid.New(), time.Minute)
	require.Error(t, err)
	require.False(t, claimed)
}

func TestReleaseChatCheckpoint_ClearsOnlyItsOwnClaim(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	claimedAt := time.Now().UTC().Truncate(time.Microsecond)
	// Matching on the claim time leaves a claim that was taken over since to its new owner.
	mock.ExpectExec("UPDATE .chats. SET .checkpoint_started_at. = NULL, .updated_at. = \\? WHERE "+
		".*.chats.\\..checkpoint_started_at. = \\?").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), claimedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, ds.ReleaseChatCheckpoint(context.Background(), uuid.New(), uuid.New(), claimedAt))
	require.NoError(t, mock.ExpectationsWereMet())
}
