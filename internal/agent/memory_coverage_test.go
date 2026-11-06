package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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

// claudeChatCtx is a chat on a native Anthropic model, whose rebuilt-context checkpoint
// calls stay on Claude.
func claudeChatCtx(memories ...string) *chatContext {
	return &chatContext{chat: &models.Chat{}, model: "claude-sonnet-4-5", modelProvider: string(models.ModelProviderAnthropic), memories: memories}
}

// Memory extraction on a rebuilt context needs the memory role's provider, not the chat's: a
// Claude chat with no OpenAI provider fails for want of OpenAI.
func TestExtractMemoriesWithScratchpadDeltaClaude_WithoutMemoryRoleProviderReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), ClaudeProvider: newHTTPTestClaudeProvider("http://127.0.0.1:1")}
	err := a.extractMemoriesWithScratchpadDeltaClaude(context.Background(), uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, claudeChatCtx(), nil)
	require.ErrorContains(t, err, "no OpenAI provider is configured")
}

func TestExtractMemoriesWithScratchpadDeltaClaude_ProviderErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	err := a.extractMemoriesWithScratchpadDeltaClaude(context.Background(), uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, claudeChatCtx(), nil)
	require.ErrorContains(t, err, "memory extraction failed")
}

// A rebuilt-context checkpoint extracts memories on the memory role's model whatever the chat
// is on, Claude included: no chat kind gets a model of its own, and an account without an
// Anthropic key still gets its memories.
func TestExtractMemoriesWithScratchpadDeltaClaude_UsesMemoryRoleModelForEveryChat(t *testing.T) {
	t.Parallel()
	chats := map[string]*chatContext{
		"native Anthropic":                claudeChatCtx("m1"),
		"Gemini":                          {chat: &models.Chat{}, model: "gemini-3.5", modelProvider: string(models.ModelProviderGoogle), memories: []string{"m1"}},
		"z.ai, Anthropic wire format":     {chat: &models.Chat{}, model: "glm-5", modelProvider: string(models.ModelProviderZAI)},
		"Anthropic-compatible connection": {chat: &models.Chat{}, model: "claude-sonnet-4-5", modelProvider: "custom", connection: &ConnectionEndpoint{Flavor: ConnectionFlavorAnthropic}},
	}
	for name, chatCtx := range chats {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", `{\"memories\":[]}`))
			a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}

			err := a.extractMemoriesWithScratchpadDeltaClaude(context.Background(), uuid.New(), uuid.New(),
				&provider.ModelContext{}, &provider.ModelContext{}, chatCtx, nil)
			require.NoError(t, err)
			got := reqs()
			require.Len(t, got, 1)
			require.Equal(t, archivalOpenAIModel, got[0]["model"])
			require.NotContains(t, got[0], "previous_response_id", "the rebuilt context is sent, not a stored response")
		})
	}
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

// A message with no text, such as an image alone, has nothing to search memories with, so no
// call is made (the agent has no provider to make one with).
func TestGetMemoryQuery_MessageWithoutTextSkipsTheCall(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	got, err := a.getMemoryQuery(context.Background(), uuid.New(), "instructions", " ")
	require.NoError(t, err)
	require.False(t, got.ShouldEnrich)
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
	_, _, err := a.getMemories(context.Background(), uuid.New(), uuid.New(), uuid.New(), "hello", false)
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
		a.applyMemoryCompactionPlan(context.Background(), uuid.New(), uuid.New(), uuid.New(), memoryCompactionPlan{}, nil, false)
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
		a.applyMemoryCompactionPlan(context.Background(), uuid.New(), uuid.New(), uuid.New(), plan, nil, false)
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// A fold's new-member origin must reach the datastore: a brand-new memory extracted in a Discord
// relay thread is written as external, with its speaker, by the real apply path.
func TestApplyMemoryCompactionPlan_NewFoldCarriesTheNewMembersOrigin(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	a := newTestAgent(ds)
	origin := models.MemoryOrigin{Provenance: models.MemoryProvenanceExternal, Speaker: "alice"}
	plan := memoryCompactionPlan{Folds: []memoryFoldPlan{{
		Group:     models.MemoryMergeGroupProposal{CanonicalContent: "alice likes tea", Scope: "Chat", Confidence: models.MemoryConfidenceHigh},
		NewOrigin: &origin,
	}}}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `memories` .*`provenance`.*`source_speaker`").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"external", "alice", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.MatchExpectationsInOrder(false)
	a.applyMemoryCompactionPlan(context.Background(), uuid.New(), uuid.New(), uuid.New(), plan, nil, false)
	// The insert is what matters here; whatever bookkeeping follows is not mocked.
	require.NoError(t, mock.ExpectationsWereMet())
}
