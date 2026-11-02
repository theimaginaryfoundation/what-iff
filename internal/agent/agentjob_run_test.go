package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// --- HandleAgentJobPromptAsync / HandleWelcomeMessagePromptAsync / handleEphemeralPromptAsync ---

func TestHandleAgentJobPromptAsync_NoUserIDInContextReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	resp, err := a.HandleAgentJobPromptAsync(context.Background(), uuid.New(), "hi", nil, nil)
	require.Nil(t, resp)
	require.ErrorContains(t, err, "user ID not found in context")
}

func TestHandleWelcomeMessagePromptAsync_NoUserIDInContextReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	resp, err := a.HandleWelcomeMessagePromptAsync(context.Background(), uuid.New(), "hi", nil, nil)
	require.Nil(t, resp)
	require.ErrorContains(t, err, "user ID not found in context")
}

func TestHandleAgentJobPromptAsync_EmptyPromptReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uuid.New())
	resp, err := a.HandleAgentJobPromptAsync(ctx, uuid.New(), "", nil, nil)
	require.Nil(t, resp)
	require.ErrorContains(t, err, "prompt is required")
}

func TestHandleAgentJobPromptAsync_CreateJobErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO .*jobs.*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uuid.New())
	resp, err := a.HandleAgentJobPromptAsync(ctx, uuid.New(), "hi", nil, nil)
	require.Nil(t, resp)
	require.ErrorContains(t, err, "failed to create background job")
	require.NoError(t, mock.ExpectationsWereMet())
}

// Note: the success path (CreateJob succeeding, background goroutine running) is not
// covered here. datastore.CreateJob's post-insert re-query loads the owner edge via
// WithOwner(), and toJobModel dereferences it unconditionally — reproducing that with
// go-sqlmock means faking a join across jobs and users tables, which is exactly the kind
// of fragile, schema-shaped mock the coverage-push guidance says to skip. The datastore
// package's own job_test.go covers CreateJob's happy path against a real in-memory
// schema; that's the right place for it, not here via a second mock stack.

// --- HandleAgentJobPrompt / HandleEphemeralPromptSync / handleEphemeralPrompt ---

func TestHandleEphemeralPromptSync_NoUserIDInContextReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	msg, err := a.HandleEphemeralPromptSync(context.Background(), uuid.New(), "hi", nil, nil)
	require.Nil(t, msg)
	require.ErrorContains(t, err, "user ID not found in context")
}

func TestHandleAgentJobPrompt_NoUserIDInContextReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	msg, err := a.HandleAgentJobPrompt(context.Background(), uuid.New(), "hi", nil, nil, nil, nil)
	require.Nil(t, msg)
	require.ErrorContains(t, err, "user ID not found in context")
}

func TestHandleEphemeralPromptSync_EmptyPromptReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uuid.New())
	msg, err := a.HandleEphemeralPromptSync(ctx, uuid.New(), "", nil, nil)
	require.Nil(t, msg)
	require.ErrorContains(t, err, "prompt is required")
}

func TestHandleEphemeralPromptSync_PrepareChatContextErrorIsReturned(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	// GetChat (inside prepareChatContext) fails, which handleEphemeralPrompt returns
	// directly without further wrapping.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	a.testHooks.ChatTurnStore = newFakeChatTurnStore() // the turn ticket stays out of sqlmock
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uuid.New())
	msg, err := a.HandleEphemeralPromptSync(ctx, uuid.New(), "hi", nil, nil)
	require.Nil(t, msg)
	require.ErrorContains(t, err, "failed to get chat")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestHandleEphemeralPrompt_KeepsCallerCancellation locks in #195: the async agent_job_run
// worker passes its cancellable run context, and cancelling it must stop the turn. With the
// context already cancelled the turn fails on its first datastore call with context.Canceled
// (no sqlmock expectations: nothing reaches the database).
func TestHandleEphemeralPrompt_KeepsCallerCancellation(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	a := newTestAgent(ds)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), middleware.UserIDKey, uuid.New()))
	cancel()
	msg, err := a.handleEphemeralPrompt(ctx, uuid.New(), "hi", nil, nil, nil, nil,
		models.ActionTypeJobRun, telemetry.CallPathAgentJob, ephemeralPromptOptions{})
	require.Nil(t, msg)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestHandleEphemeralPromptSync_DetachesFromCallerCancellation checks the sync entry points
// still run detached: a caller context that has already ended does not stop the turn, which
// here reaches the database (and fails there on the mocked GetChat instead).
func TestHandleEphemeralPromptSync_DetachesFromCallerCancellation(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]func(a *Agent, ctx context.Context) (*models.ChatMessage, error){
		"HandleEphemeralPromptSync": func(a *Agent, ctx context.Context) (*models.ChatMessage, error) {
			return a.HandleEphemeralPromptSync(ctx, uuid.New(), "hi", nil, nil)
		},
		"HandleAgentJobPrompt": func(a *Agent, ctx context.Context) (*models.ChatMessage, error) {
			return a.HandleAgentJobPrompt(ctx, uuid.New(), "hi", nil, nil, nil, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds, mock, cleanup := newTestDatastore(t)
			defer cleanup()
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
			mock.ExpectRollback()

			a := newTestAgent(ds)
			a.testHooks.ChatTurnStore = newFakeChatTurnStore() // the turn ticket stays out of sqlmock
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), middleware.UserIDKey, uuid.New()))
			cancel()
			msg, err := run(a, ctx)
			require.Nil(t, msg)
			require.ErrorContains(t, err, "failed to get chat")
			require.NotErrorIs(t, err, context.Canceled)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// A scheduled run's ticket is stoppable like a user turn's job (ADR 0x039): the thread's Stop
// finds it among the in-process running jobs and cancels the run's context; once the reply is
// saved (release) a late Stop no longer reaches it.
func TestStoppableTurnTicket(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()
	userID := uuid.New()

	t.Run("no ticket", func(t *testing.T) {
		parent := context.Background()
		ctx, release, stop := a.stoppableTurnTicket(parent, userID, nil)
		require.Equal(t, parent, ctx)
		release()
		stop()
		require.Empty(t, a.runningJobCancels)
	})

	t.Run("stop reaches the run", func(t *testing.T) {
		ticket := &models.Job{ID: uuid.New(), UserID: userID}
		ctx, release, stop := a.stoppableTurnTicket(context.Background(), userID, ticket)
		defer stop()
		defer release()
		require.True(t, a.hasRunningJob(ticket.ID))
		require.NoError(t, a.cancelRunningJob(userID, ticket.ID))
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	})

	t.Run("released after the reply", func(t *testing.T) {
		ticket := &models.Job{ID: uuid.New(), UserID: userID}
		ctx, release, stop := a.stoppableTurnTicket(context.Background(), userID, ticket)
		release()
		release() // the turn's deferred cleanup releases again
		require.False(t, a.hasRunningJob(ticket.ID))
		require.NoError(t, a.cancelRunningJob(userID, ticket.ID))
		require.NoError(t, ctx.Err(), "post-processing keeps running")
		stop()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	})
}

func TestStoppedTurnError(t *testing.T) {
	t.Parallel()
	live := context.Background()
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	providerErr := errors.New("provider call failed")

	require.NoError(t, stoppedTurnError(stopped, nil))
	require.Equal(t, providerErr, stoppedTurnError(live, providerErr), "a live run's failure stays a failure")
	got := stoppedTurnError(stopped, providerErr)
	require.ErrorIs(t, got, context.Canceled, "a stopped run reports the stop")
	require.ErrorIs(t, got, providerErr)
	require.Equal(t, context.Canceled, stoppedTurnError(stopped, context.Canceled))
}
