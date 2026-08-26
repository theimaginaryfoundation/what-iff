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

// --- memoryExtractionSchemaMapForClaude ---

func TestMemoryExtractionSchemaMapForClaude_ReturnsNonEmptyMap(t *testing.T) {
	t.Parallel()
	out := memoryExtractionSchemaMapForClaude()
	require.NotEmpty(t, out)
	require.Contains(t, out, "properties")
}

// --- extractMemoriesWithScratchpadDelta ---

func TestExtractMemoriesWithScratchpadDelta_NilResponseIDIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.extractMemoriesWithScratchpadDelta(context.Background(), uuid.New(), uuid.New(), nil, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}}, nil)
	})
}

func TestExtractMemoriesWithScratchpadDelta_ProviderErrorLogsAndReturns(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	respID := "resp_123"
	require.NotPanics(t, func() {
		a.extractMemoriesWithScratchpadDelta(context.Background(), uuid.New(), uuid.New(), &respID, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}}, nil)
	})
}

func TestExtractMemoriesWithScratchpadDelta_UnmarshalErrorLogsAndReturns(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", "not valid json"))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	respID := "resp_123"
	require.NotPanics(t, func() {
		a.extractMemoriesWithScratchpadDelta(context.Background(), uuid.New(), uuid.New(), &respID, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}}, nil)
	})
}

func TestExtractMemoriesWithScratchpadDelta_SuccessWithNoMemoriesIsNoOpDownstream(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", `{\"memories\":[]}`))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	respID := "resp_123"
	require.NotPanics(t, func() {
		a.extractMemoriesWithScratchpadDelta(context.Background(), uuid.New(), uuid.New(), &respID, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}, memories: []string{"m1"}}, nil)
	})
}

// --- extractMemoriesWithScratchpadDeltaClaude ---

func newHTTPTestClaudeProvider(baseURL string) *provider.ClaudeProvider {
	return provider.NewClaudeProviderWithBaseURL("test-key", baseURL, nil, nil)
}

func claudeMessageTextJSONBody(id, text string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"text","text":"` + text + `"}],"stop_reason":"end_turn","stop_sequence":null,` +
		`"usage":{"input_tokens":5,"output_tokens":7}}`
}

func TestExtractMemoriesWithScratchpadDeltaClaude_NilProviderReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	err := a.extractMemoriesWithScratchpadDeltaClaude(context.Background(), uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}}, nil)
	require.ErrorContains(t, err, "ClaudeProvider is nil")
}

func TestExtractMemoriesWithScratchpadDeltaClaude_ProviderErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), ClaudeProvider: newHTTPTestClaudeProvider(srv.URL)}
	err := a.extractMemoriesWithScratchpadDeltaClaude(context.Background(), uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}}, nil)
	require.ErrorContains(t, err, "Claude memory extraction failed")
}

func TestExtractMemoriesWithScratchpadDeltaClaude_SuccessWithNoMemoriesIsNoOpDownstream(t *testing.T) {
	t.Parallel()
	body := claudeMessageTextJSONBody("msg_1", `{\"memories\":[]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), ClaudeProvider: newHTTPTestClaudeProvider(srv.URL)}
	err := a.extractMemoriesWithScratchpadDeltaClaude(context.Background(), uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, &chatContext{chat: &models.Chat{}, memories: []string{"m1"}}, nil)
	require.NoError(t, err)
}

// --- getMemoryQuery ---

func TestGetMemoryQuery_ProviderErrorIsReturned(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.getMemoryQuery(context.Background(), uuid.New(), "instructions", "user message")
	require.Error(t, err)
}

func TestGetMemoryQuery_UnmarshalErrorIsReturned(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", "not valid json"))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.getMemoryQuery(context.Background(), uuid.New(), "instructions", "user message")
	require.Error(t, err)
}

func TestGetMemoryQuery_SuccessParsesQuery(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", `{\"query\":\"likes Go\",\"should_enrich\":true}`))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got, err := a.getMemoryQuery(context.Background(), uuid.New(), "instructions", "user message")
	require.NoError(t, err)
	require.True(t, got.ShouldEnrich)
	require.Equal(t, "likes Go", got.Query)
}

// --- getMemories ---

func TestGetMemories_GetUserByIDErrorIsReturned(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	_, _, err := a.getMemories(context.Background(), uuid.New(), uuid.New(), uuid.New(), "hello")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- compactMemoriesFromCheckpoint ---

func TestCompactMemoriesFromCheckpoint_NoCandidatesIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.compactMemoriesFromCheckpoint(context.Background(), uuid.New(), uuid.New(), uuid.New(),
			&provider.ModelContext{}, &chatContext{chat: &models.Chat{}}, nil, nil)
	})
}

func TestCompactMemoriesFromCheckpoint_NilChatCtxIsHandled(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.compactMemoriesFromCheckpoint(context.Background(), uuid.New(), uuid.New(), uuid.New(),
			&provider.ModelContext{}, nil, nil, nil)
	})
}

// --- applyMemoryCompactionPlan ---

func TestApplyMemoryCompactionPlan_EmptyPlanIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.applyMemoryCompactionPlan(context.Background(), uuid.New(), uuid.New(), uuid.New(), memoryCompactionPlan{}, nil)
	})
}

func TestApplyMemoryCompactionPlan_LinkWithTooFewMembersIsSkipped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	// No mock expectations: a single existing ID and no new members is < 2 total, so the
	// link is skipped before any datastore call is made.
	a := newTestAgent(ds)
	plan := memoryCompactionPlan{
		Links: []memoryLinkPlan{
			{ExistingIDs: []uuid.UUID{uuid.New()}},
		},
	}
	require.NotPanics(t, func() {
		a.applyMemoryCompactionPlan(context.Background(), uuid.New(), uuid.New(), uuid.New(), plan, nil)
	})
	require.NoError(t, mock.ExpectationsWereMet())
}
