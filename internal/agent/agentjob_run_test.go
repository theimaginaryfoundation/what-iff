package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
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
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uuid.New())
	msg, err := a.HandleEphemeralPromptSync(ctx, uuid.New(), "hi", nil, nil)
	require.Nil(t, msg)
	require.ErrorContains(t, err, "failed to get chat")
	require.NoError(t, mock.ExpectationsWereMet())
}
