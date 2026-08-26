package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// --- beginCompactionEvent ---

func TestBeginCompactionEvent_NilChatCtxReturnsNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	got := a.beginCompactionEvent(context.Background(), uuid.New(), nil, nil, uuid.New(), "openai", "checkpoint", uuid.New(), "scratch", true)
	require.Nil(t, got)
}

func TestBeginCompactionEvent_CreateErrorReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin().WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{}}
	got := a.beginCompactionEvent(context.Background(), uuid.New(), chatCtx, nil, uuid.New(), "openai", "checkpoint", uuid.Nil, "scratch", true)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- finishCompactionEvent ---

func TestFinishCompactionEvent_NilEventIDIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.finishCompactionEvent(context.Background(), uuid.New(), nil, "summary")
	})
}

func TestFinishCompactionEvent_ErrorIsLoggedNotPanicked(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin().WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	eventID := uuid.New()
	require.NotPanics(t, func() {
		a.finishCompactionEvent(context.Background(), uuid.New(), &eventID, "summary")
	})
	require.NoError(t, mock.ExpectationsWereMet())
}
