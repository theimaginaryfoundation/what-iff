package agent

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A checkpoint that is due while another is running for the chat is skipped: the next turn no
// longer waits for the running one, so this is what stops a second, duplicate summary pass.
func TestClaimCheckpoint_SkipsWhileAnotherIsRunning(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 0))

	a := newTestAgent(ds)
	release, ok := a.claimCheckpoint(context.Background(), uuid.New(), uuid.New(), checkpointDecision{Trigger: checkpointTriggerTurnCount})
	require.False(t, ok)
	require.Nil(t, release)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimCheckpoint_RunsAndReleasesTheClaim(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*checkpoint_started_at.*").WillReturnResult(sqlmock.NewResult(0, 1)) // claim
	mock.ExpectExec("UPDATE .*chats.*checkpoint_started_at.*").WillReturnResult(sqlmock.NewResult(0, 1)) // release

	a := newTestAgent(ds)
	// The turn's context ending must not leave the claim behind.
	ctx, cancel := context.WithCancel(context.Background())
	release, ok := a.claimCheckpoint(ctx, uuid.New(), uuid.New(), checkpointDecision{})
	require.True(t, ok)
	cancel()
	release()
	require.NoError(t, mock.ExpectationsWereMet())
}

// If the claim cannot be taken (a database error), the checkpoint is skipped, not run unguarded:
// the next turn's check retries it.
func TestClaimCheckpoint_SkipsWhenTheClaimFails(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	_, ok := a.claimCheckpoint(context.Background(), uuid.New(), uuid.New(), checkpointDecision{})
	require.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}
