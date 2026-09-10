package agent

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// --- loadImageBytesForClaude ---

func TestLoadImageBytesForClaude_NoAttachmentsReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}

	got := a.loadImageBytesForClaude(context.Background(), uuid.New(), chatMessage)
	require.NotNil(t, got)
	require.Empty(t, got)
}

// --- HandleUserMessage ---

func TestHandleUserMessage_NoUserIDInContextReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	resp, err := a.HandleUserMessage(context.Background(), models.ChatMessage{})
	require.Nil(t, resp)
	require.ErrorContains(t, err, "user ID not found in context")
}

func TestHandleUserMessage_CreateChatMessageErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop()}
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, userID)

	resp, err := a.HandleUserMessage(ctx, models.ChatMessage{ChatID: uuid.New(), Message: "hi"})
	require.Nil(t, resp)
	require.ErrorContains(t, err, "failed to create chat message")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- RetryUserChatMessage ---

func TestRetryUserChatMessage_NoUserIDInContextReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	resp, err := a.RetryUserChatMessage(context.Background(), uuid.New(), uuid.New())
	require.Nil(t, resp)
	require.ErrorContains(t, err, "user ID not found in context")
}

func TestRetryUserChatMessage_GetChatMessageErrorIsReturned(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop()}
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, userID)

	resp, err := a.RetryUserChatMessage(ctx, uuid.New(), uuid.New())
	require.Nil(t, resp)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- handleUserMessage ---

func TestHandleUserMessage_UpdateJobStatusFailureReturnsEarly(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	// First exist-check failure drives updateJobStatus's UpdateJob call to fail.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	// setJobStatusFailed's own UpdateJob call also fails its exist check, so it
	// logs and returns without touching draft deltas or the user message.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop()}
	chatJob := &models.Job{ID: uuid.New(), UserID: uuid.New(), JobType: JobTypeChatMessage, Status: models.JobStatusPending}
	chatMessage := &models.ChatMessage{ID: uuid.New(), ChatID: uuid.New()}

	msg, err := a.handleUserMessage(context.Background(), chatJob, chatMessage)
	require.Nil(t, msg)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- prepareChatContext ---

func TestPrepareChatContext_GetChatErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}

	got, err := a.prepareChatContext(context.Background(), uuid.New(), chatMessage)
	require.Nil(t, got)
	require.ErrorContains(t, err, "failed to get chat")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- runUserChatPostInferencePhases ---

func TestRunUserChatPostInferencePhases_NilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.runUserChatPostInferencePhases(context.Background(), nil, &models.ChatMessage{}, &models.ChatMessage{}, &chatContext{}, &provider.ModelContext{}, metering.Decision{})
	})
}

func TestRunUserChatPostInferencePhases_TerminalJobSkipsPhases(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatJob := &models.Job{ID: uuid.New(), UserID: uuid.New(), Status: models.JobStatusFailed}
	require.NotPanics(t, func() {
		a.runUserChatPostInferencePhases(context.Background(), chatJob, &models.ChatMessage{}, &models.ChatMessage{}, &chatContext{}, &provider.ModelContext{}, metering.Decision{})
	})

	chatJob.Status = models.JobStatusCancelled
	require.NotPanics(t, func() {
		a.runUserChatPostInferencePhases(context.Background(), chatJob, &models.ChatMessage{}, &models.ChatMessage{}, &chatContext{}, &provider.ModelContext{}, metering.Decision{})
	})
}

// --- finalizeChat / postMessageProcessing ---

// TestFinalizeChat_NonDefaultNameSkipsRenameAndChecksMockMode drives finalizeChat with a
// non-default chat name (skipping generateChatName) into postMessageProcessing under mock
// mode, which records usage and then returns before any checkpoint-related datastore calls
// (checkpointing needs real provider calls and is deliberately skipped under mock/local LLM
// backends). No datastore mocking is needed since NoopMeter.Record and the mock-mode early
// return never touch a.ds.
func TestFinalizeChat_NonDefaultNameSkipsRenameAndChecksMockMode(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), mockLLM: true, meter: metering.NoopMeter{Logger: zap.NewNop()}}
	chatMessage := &models.ChatMessage{ChatID: uuid.New(), Message: "hi"}
	agentMessage := &models.ChatMessage{ID: uuid.New()}
	chatCtx := &chatContext{
		chat:  &models.Chat{Name: "Not Default", ID: uuid.New()},
		model: "mock-model",
	}

	require.NotPanics(t, func() {
		a.finalizeChat(context.Background(), uuid.New(), chatMessage, agentMessage, chatCtx, &provider.ModelContext{}, metering.Decision{Allowed: true})
	})
}

// --- runCheckpointOpenAI ---

func TestRunCheckpointOpenAI_NilAgentMessageIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := &chatContext{chat: &models.Chat{}}

	require.NotPanics(t, func() {
		a.runCheckpointOpenAI(context.Background(), uuid.New(), chatMessage, nil, chatCtx, 1, &provider.ModelContext{}, "reason")
	})
}

func TestRunCheckpointOpenAI_EmptyResponseIDIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := &chatContext{chat: &models.Chat{}}
	empty := ""
	agentMessage := &models.ChatMessage{ID: uuid.New(), ResponseID: &empty}

	require.NotPanics(t, func() {
		a.runCheckpointOpenAI(context.Background(), uuid.New(), chatMessage, agentMessage, chatCtx, 1, &provider.ModelContext{}, "reason")
	})
}

// --- persistCheckpointSummary ---

func TestPersistCheckpointSummary_DatastoreErrorReturnsFalse(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*chats.*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop()} // memoryTool nil skips the embedding/upsert step entirely.
	ok := a.persistCheckpointSummary(context.Background(), uuid.New(), uuid.New(), "summary text", 3, "OpenAI", uuid.New())
	require.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}
