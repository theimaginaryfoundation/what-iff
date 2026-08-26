package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// --- updateScratchpad ---

func TestUpdateScratchpad_NilResponseIDReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.updateScratchpad(context.Background(), uuid.New(), nil, &chatContext{chat: &models.Chat{}})
	require.ErrorContains(t, err, "response ID is nil")
}

func TestUpdateScratchpad_GetPersonalityErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	respID := "resp_1"
	_, err := a.updateScratchpad(context.Background(), uuid.New(), &respID, &chatContext{chat: &models.Chat{}})
	require.ErrorContains(t, err, "failed to get personality by ID")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- summarizeScratchpad ---

func TestSummarizeScratchpad_NilResponseIDReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.summarizeScratchpad(context.Background(), uuid.New(), nil, &chatContext{chat: &models.Chat{}}, "text")
	require.ErrorContains(t, err, "response ID is nil")
}

func TestSummarizeScratchpad_NoPersonalityReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	respID := "resp_1"
	_, err := a.summarizeScratchpad(context.Background(), uuid.New(), &respID, &chatContext{chat: &models.Chat{PersonalityID: uuid.Nil}}, "text")
	require.ErrorContains(t, err, "not available without a personality")
}

// --- updateScratchpadClaude ---

func TestUpdateScratchpadClaude_GetPersonalityErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	_, err := a.updateScratchpadClaude(context.Background(), uuid.New(), &chatContext{chat: &models.Chat{}}, &provider.ModelContext{})
	require.ErrorContains(t, err, "failed to get personality by ID")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- summarizeScratchpadClaude ---

func TestSummarizeScratchpadClaude_ProviderErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.summarizeScratchpadClaude(context.Background(), uuid.New(), "text")
	require.ErrorContains(t, err, "failed to summarize Claude scratchpad")
}

// --- scratchpadPromptOrDefault ---

func TestScratchpadPromptOrDefault_NilPersonalityReturnsDefault(t *testing.T) {
	t.Parallel()
	require.Equal(t, scratchpadUpdatePrompt, scratchpadPromptOrDefault(nil))
}
