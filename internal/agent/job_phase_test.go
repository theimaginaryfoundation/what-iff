package agent

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// --- advanceChatJobStatus ---

func TestAdvanceChatJobStatus_NilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NoError(t, a.advanceChatJobStatus(context.Background(), nil, models.JobStatusExpressionComplete))
}

func TestAdvanceChatJobStatus_UpdateErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), Status: models.JobStatusProcessing}
	err := a.advanceChatJobStatus(context.Background(), job, models.JobStatusExpressionComplete)
	require.Error(t, err)
	require.ErrorContains(t, err, "update job status")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- persistInferencePhase ---

func TestPersistInferencePhase_NilChatJobReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	err := a.persistInferencePhase(context.Background(), nil, &models.ChatMessage{}, nil, nil, nil, &models.ChatMessage{}, false)
	require.ErrorContains(t, err, "chatJob is nil")
}

func TestPersistInferencePhase_NilChatMessageReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	err := a.persistInferencePhase(context.Background(), &models.Job{}, nil, nil, nil, nil, &models.ChatMessage{}, false)
	require.ErrorContains(t, err, "chatMessage is nil")
}

func TestPersistInferencePhase_NilAgentMessageReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	err := a.persistInferencePhase(context.Background(), &models.Job{}, &models.ChatMessage{}, nil, nil, nil, nil, false)
	require.ErrorContains(t, err, "agentMessage is nil")
}

func TestPersistInferencePhase_UpdateJobErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), Status: models.JobStatusProcessing}
	err := a.persistInferencePhase(context.Background(), job, &models.ChatMessage{}, nil, nil, nil, &models.ChatMessage{ID: uuid.New()}, false)
	require.ErrorContains(t, err, "failed to update job")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- persistUserTurnAndChatAfterInference ---

func TestPersistUserTurnAndChatAfterInference_SkipsUserTurnUpdatesTriesChatUpdate(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	// persistUserTurnUpdate is false, so only the chat-continuity branch runs;
	// UpdateChat's exists-check reports the chat missing, which the function
	// only logs (best-effort), so no panic and no error surfaces.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	require.NotPanics(t, func() {
		a.persistUserTurnAndChatAfterInference(context.Background(), uuid.New(), &models.ChatMessage{}, &models.Chat{ID: uuid.New()}, nil, &provider.GenerateResponse{ID: "r1", CreatedAt: 1}, false)
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPersistUserTurnAndChatAfterInference_NilResultSkipsBothUpdates(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.persistUserTurnAndChatAfterInference(context.Background(), uuid.New(), &models.ChatMessage{}, &models.Chat{}, nil, nil, true)
	})
}

func TestPersistUserTurnAndChatAfterInference_NilChatMessageAndChatAreNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.persistUserTurnAndChatAfterInference(context.Background(), uuid.New(), nil, nil, nil, &provider.GenerateResponse{ID: "r1"}, true)
	})
}

// --- applyExpressionPhase ---

func TestApplyExpressionPhase_NilChatCtxNilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	err := a.applyExpressionPhase(context.Background(), uuid.New(), nil, nil, nil, "hi", &models.ChatMessage{})
	require.NoError(t, err)
}

func TestApplyExpressionPhase_NilChatCtxAdvancesJob(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), Status: models.JobStatusProcessing}
	err := a.applyExpressionPhase(context.Background(), uuid.New(), job, nil, nil, "hi", &models.ChatMessage{})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyExpressionPhase_DisabledExpressionsNilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{chat: &models.Chat{}, expressionsEnabled: false}
	err := a.applyExpressionPhase(context.Background(), uuid.New(), nil, chatCtx, nil, "hi", &models.ChatMessage{})
	require.NoError(t, err)
}

func TestApplyExpressionPhase_NonVendorLLMNilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), mockLLM: true}
	chatCtx := &chatContext{chat: &models.Chat{}, expressionsEnabled: true}
	err := a.applyExpressionPhase(context.Background(), uuid.New(), nil, chatCtx, nil, "hi", &models.ChatMessage{})
	require.NoError(t, err)
}

func TestApplyExpressionPhase_NilPersonalityIDSkipsPickerAndAdvances(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), Status: models.JobStatusProcessing}
	chatCtx := &chatContext{chat: &models.Chat{PersonalityID: uuid.Nil}, expressionsEnabled: true}
	err := a.applyExpressionPhase(context.Background(), uuid.New(), job, chatCtx, &provider.ModelContext{}, "hi", &models.ChatMessage{ID: uuid.New(), Message: "reply"})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- advanceJobInferenceComplete ---

func TestAdvanceJobInferenceComplete_NilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NoError(t, a.advanceJobInferenceComplete(context.Background(), nil, uuid.New()))
}

func TestAdvanceJobInferenceComplete_UpdateErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), Status: models.JobStatusProcessing}
	err := a.advanceJobInferenceComplete(context.Background(), job, uuid.New())
	require.ErrorContains(t, err, "failed to update job")
	require.NoError(t, mock.ExpectationsWereMet())
}
