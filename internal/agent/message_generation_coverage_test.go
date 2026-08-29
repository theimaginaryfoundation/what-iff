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

// errServer returns an httptest server that answers every request with a 400,
// so any adapter routed through it fails fast on the wire (no successful
// generation, so no downstream ds.CreateChatMessage call is ever reached).
func errServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
}

func baseChatCtxForGeneration(model, modelProvider string) *chatContext {
	return &chatContext{
		chat:          &models.Chat{ToolsEnabled: false},
		model:         model,
		modelProvider: modelProvider,
	}
}

// --- generateAssistantForMessageOpenAI ---

func TestGenerateAssistantForMessageOpenAI_AdapterErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := errServer(t)
	defer srv.Close()

	a := &Agent{
		logger:         zap.NewNop(),
		OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL),
	}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("gpt-5.1", "openai")

	_, _, err := a.generateAssistantForMessageOpenAI(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.Error(t, err)
	require.ErrorContains(t, err, "OpenAI agent loop failed")
}

// --- generateAssistantForMessageClaude ---

func TestGenerateAssistantForMessageClaude_NoProviderReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("claude-haiku-4-5", "anthropic")

	_, _, err := a.generateAssistantForMessageClaude(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "ANTHROPIC_API_KEY is not configured")
}

func TestGenerateAssistantForMessageClaude_AdapterErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := errServer(t)
	defer srv.Close()

	a := &Agent{
		logger:         zap.NewNop(),
		ClaudeProvider: newHTTPTestClaudeProvider(srv.URL),
	}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("claude-haiku-4-5", "anthropic")

	_, _, err := a.generateAssistantForMessageClaude(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "Claude agent loop failed")
}

// --- generateAssistantForMessageGemini ---

func TestGenerateAssistantForMessageGemini_NoProviderReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("gemini-3.5", "google")

	_, _, err := a.generateAssistantForMessageGemini(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "GEMINI_API_KEY is not configured")
}

func TestGenerateAssistantForMessageGemini_AdapterErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := errServer(t)
	defer srv.Close()

	a := &Agent{
		logger:         zap.NewNop(),
		GeminiProvider: provider.NewGeminiProvider("gemini-key", srv.URL, nil, nil),
	}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("gemini-3.5", "google")

	_, _, err := a.generateAssistantForMessageGemini(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "Gemini agent loop failed")
}

// --- generateAssistantForMessageLocal ---

func TestGenerateAssistantForMessageLocal_NoProviderReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("local-model", "local")

	_, _, err := a.generateAssistantForMessageLocal(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "no local model server is configured")
}

func TestGenerateAssistantForMessageLocal_AdapterErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := errServer(t)
	defer srv.Close()

	a := &Agent{
		logger:        zap.NewNop(),
		LocalProvider: provider.NewLocalProvider(srv.URL, nil, nil),
		localLLMModel: "local-model",
	}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("local-model", "local")

	_, _, err := a.generateAssistantForMessageLocal(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "local model agent loop failed")
}

// --- generateAssistantForMessageOpenAIChatCompletions ---

func TestGenerateAssistantForMessageOpenAIChatCompletions_NoProviderReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("mistral-large-latest", "mistral")

	_, _, err := a.generateAssistantForMessageOpenAIChatCompletions(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "MISTRAL_API_KEY is not configured")
}

func TestGenerateAssistantForMessageOpenAIChatCompletions_AdapterErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := errServer(t)
	defer srv.Close()

	a := &Agent{
		logger:          zap.NewNop(),
		MistralProvider: provider.NewMistralProvider("mistral-key", srv.URL, nil, nil),
	}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("mistral-large-latest", "mistral")

	_, _, err := a.generateAssistantForMessageOpenAIChatCompletions(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.Error(t, err)
}

// --- dispatchAssistantGeneration routing ---

func TestDispatchAssistantGeneration_RoutesMockLLM(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop(), mockLLM: true}
	chatMessage := &models.ChatMessage{ChatID: uuid.New(), Message: "hi"}
	chatCtx := baseChatCtxForGeneration("mock-model", "openai")

	_, _, err := a.dispatchAssistantGeneration(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDispatchAssistantGeneration_RoutesLocalLLM(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), localLLM: true}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("local-model", "local")

	_, _, err := a.dispatchAssistantGeneration(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "no local model server is configured")
}

func TestDispatchAssistantGeneration_RoutesGemini(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("gemini-3.5", string(models.ModelProviderGoogle))

	_, _, err := a.dispatchAssistantGeneration(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "GEMINI_API_KEY is not configured")
}

func TestDispatchAssistantGeneration_RoutesOpenAIChatCompletions(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("mistral-large-latest", string(models.ModelProviderMistral))

	_, _, err := a.dispatchAssistantGeneration(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "MISTRAL_API_KEY is not configured")
}

func TestDispatchAssistantGeneration_RoutesClaude(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("claude-haiku-4-5", string(models.ModelProviderAnthropic))

	_, _, err := a.dispatchAssistantGeneration(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "ANTHROPIC_API_KEY is not configured")
}

func TestDispatchAssistantGeneration_RoutesOpenAIDefault(t *testing.T) {
	t.Parallel()
	srv := errServer(t)
	defer srv.Close()
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}
	chatCtx := baseChatCtxForGeneration("gpt-5.1", string(models.ModelProviderOpenAI))

	_, _, err := a.dispatchAssistantGeneration(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, &provider.ModelContext{})
	require.ErrorContains(t, err, "OpenAI agent loop failed")
}

// --- generateAssistantForMessageMock / runGeneration / generateAssistantForMessage / saveAgentResponse ---

// TestGenerateAssistantForMessageMock_SaveFailurePropagates drives the mock adapter through
// the real runGeneration -> saveAgentResponse pipeline. The mock adapter always succeeds at
// "generation" (it has no failure injection point), so the only way to reach an error return
// without a full ent/sqlmock happy-path chain is to make the final persistence step
// (ds.CreateChatMessage) fail. This still exercises generateAssistantForMessage,
// dispatchAssistantGeneration's mock branch, generateAssistantForMessageMock, runGeneration
// (draft buffer, tool-call merge, personality/mood resolution), and saveAgentResponse's error
// wrapping in one pass.
func TestGenerateAssistantForMessageMock_SaveFailurePropagates(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop(), mockLLM: true}
	chatJob := &models.Job{UserID: uuid.New()} // zero ID keeps the draft buffer inert (no DB writes for deltas)
	chatMessage := &models.ChatMessage{ChatID: uuid.New(), Message: "hello"}
	chatCtx := baseChatCtxForGeneration("mock-model", "openai")

	agentMessage, result, err := a.generateAssistantForMessage(context.Background(), uuid.New(), chatJob, chatMessage, chatCtx, &provider.ModelContext{})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to save mock agent response")
	require.Nil(t, agentMessage)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGenerateAssistantForMessageMock_ImageRitualBranch proves that a chat message carrying
// the image-generate system ritual routes to handleImageGenerateRitual instead of the normal
// mock-adapter echo pipeline, even under mock mode. Uses the same test-hook seam as
// TestHandleImageGenerateRitual_MockMode_PersistsFixturePNG so no real datastore is needed.
func TestGenerateAssistantForMessageMock_ImageRitualBranch(t *testing.T) {
	t.Parallel()
	chatID := uuid.New()
	a := &Agent{logger: zap.NewNop(), mockLLM: true, testHooks: agentTestHooks{
		ImageRitualCreateChatMessage: func(_ context.Context, _ uuid.UUID, cm models.ChatMessage) (*models.ChatMessage, error) {
			return &models.ChatMessage{ID: uuid.New(), ChatID: chatID, Message: cm.Message, Origin: cm.Origin, ToolCalls: cm.ToolCalls}, nil
		},
		ImageRitualCreateFileAttachment: func(_ context.Context, _ uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
			return &models.FileAttachment{ID: uuid.New(), Name: fa.Name, FileType: fa.FileType}, nil
		},
		ImageRitualPersistImage: func(_ context.Context, _ uuid.UUID, _ *models.FileAttachment, _ string) error {
			return nil
		},
	}}
	chatMessage := &models.ChatMessage{
		ChatID:  chatID,
		Message: "draw a fox",
		Rituals: []*models.Ritual{{ID: SystemRitualIDImageGenerate}},
	}
	chatCtx := &chatContext{model: models.DefaultModelName, chat: &models.Chat{ID: chatID}}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "context", false)

	agentMessage, result, err := a.generateAssistantForMessageMock(context.Background(), uuid.New(), &models.Job{}, chatMessage, chatCtx, mc)
	require.NoError(t, err)
	require.NotNil(t, agentMessage)
	require.NotNil(t, result)
	require.Contains(t, result.Text, "draw a fox")
}

// --- saveAgentResponse ---

func TestSaveAgentResponse_CreateChatMessageErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := &Agent{ds: ds, logger: zap.NewNop()}
	result := &provider.GenerateResponse{ID: "resp_1", Text: "hi"}

	msg, err := a.saveAgentResponse(context.Background(), uuid.New(), uuid.New(), result, nil, nil, "gpt-5.1", "", nil)
	require.Nil(t, msg)
	require.ErrorContains(t, err, "failed to create chat message")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- openAIResponseParamsForChat ---

func TestOpenAIResponseParamsForChat_ToolsDisabled(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{
		chat:  &models.Chat{ToolsEnabled: false},
		model: "gpt-5.1",
	}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}

	params := a.openAIResponseParamsForChat(context.Background(), chatCtx, uuid.New(), chatMessage, &provider.ModelContext{})
	require.Equal(t, "gpt-5.1", string(params.Model))
	require.False(t, bool(params.ParallelToolCalls.Value))
	require.Empty(t, params.Tools)
	require.Empty(t, params.Include)
}
