package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// chatCompletionsRecorder is a fake OpenAI-compatible Chat Completions endpoint. It
// records each request and answers with a fixed response.
type chatCompletionsRecorder struct {
	srv *httptest.Server

	mu       sync.Mutex
	paths    []string
	bodies   []map[string]any
	authHdrs []string
}

func newChatCompletionsRecorder(t *testing.T, status int, respBody string) *chatCompletionsRecorder {
	t.Helper()
	rec := &chatCompletionsRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.bodies = append(rec.bodies, body)
		rec.authHdrs = append(rec.authHdrs, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *chatCompletionsRecorder) requests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func chatCompletionTextBody(text string) string {
	return `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m",` +
		`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"` + text + `"}}],` +
		`"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`
}

func subagentTestSpecs() []agenttools.FunctionToolSpec {
	return []agenttools.FunctionToolSpec{{
		Name:        "weather__lookup",
		Description: "look up the weather",
		Properties:  map[string]any{"city": map[string]any{"type": "string"}},
		Required:    []string{"city"},
	}}
}

// toolNames returns the function names in a recorded request's "tools" array.
func toolNames(body map[string]any) []string {
	var out []string
	tools, _ := body["tools"].([]any)
	for _, raw := range tools {
		entry, _ := raw.(map[string]any)
		fn, _ := entry["function"].(map[string]any)
		if name, ok := fn["name"].(string); ok {
			out = append(out, name)
		}
	}
	return out
}

func newSubagentChatCompletionsAgent(baseURL string) *Agent {
	return &Agent{
		logger:           zap.NewNop(),
		QwenProvider:     provider.NewQwenProvider("qwen-key", baseURL, nil, nil),
		MistralProvider:  provider.NewMistralProvider("mistral-key", baseURL, nil, nil),
		DeepSeekProvider: provider.NewDeepSeekProvider("deepseek-key", baseURL, nil, nil),
		XiaomiProvider:   provider.NewXiaomiProvider("xiaomi-key", baseURL, nil, nil),
		GeminiProvider:   provider.NewGeminiProvider("google-key", baseURL, nil, nil),
	}
}

// Regression for #238: Qwen (and the other Chat Completions providers) used to be
// rejected with "<provider> subagent calls are not yet supported".
func TestRunSubagentChatCompletions_PreviouslyRejectedProvidersSucceed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider string
		model    string
	}{
		{"qwen", "qwen3.7-plus"},
		{"mistral", "mistral-large-latest"},
		{"deepseek", "deepseek-chat"},
		{"xiaomi", "mimo-v2.5-pro"},
		{"google", "gemini-3.5-flash"},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("hello from "+tc.provider))
			a := newSubagentChatCompletionsAgent(rec.srv.URL)
			caps := subagentModelCapabilities{Provider: tc.provider, ToolSupport: true}

			out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(), caps, tc.model,
				buildSubagentModelContext("be brief", "", "say hi"), nil, nil)

			require.NoError(t, err)
			require.Equal(t, "hello from "+tc.provider, out.Output)
			require.Equal(t, int64(11), out.InputTokens)
			require.Equal(t, 1, rec.requests())
			require.Equal(t, tc.model, rec.bodies[0]["model"])
			require.Contains(t, rec.paths[0], "/chat/completions")
			require.Equal(t, "Bearer "+tc.provider+"-key", rec.authHdrs[0], "must authenticate with that provider's own key")
			require.Empty(t, toolNames(rec.bodies[0]), "no skills means no tools are sent")
		})
	}
}

func TestRunSubagentChatCompletions_QwenSendsSkillToolsWhenToolCapable(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("done"))
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", ToolSupport: true}, "qwen3.7-plus",
		buildSubagentModelContext("", "", "weather?"), subagentTestSpecs(), nil)

	require.NoError(t, err)
	require.Equal(t, "done", out.Output)
	require.Equal(t, []string{"weather__lookup"}, toolNames(rec.bodies[0]))
}

// A model not flagged tool_support must get a specific error instead of silently
// losing the skills' tools or being sent tools it cannot call.
func TestRunSubagentChatCompletions_ToolsRejectedWhenModelLacksToolSupport(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("unused"))
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", ToolSupport: false}, "qwen-turbo",
		buildSubagentModelContext("", "", "weather?"), subagentTestSpecs(), nil)

	require.Nil(t, out)
	require.ErrorContains(t, err, `qwen model "qwen-turbo" does not support tool calling`)
	require.ErrorContains(t, err, "skill_ids")
	require.NotContains(t, err.Error(), "not yet supported")
	require.Zero(t, rec.requests(), "an unsupported request must never reach the provider")
}

func TestRunSubagentChatCompletions_NoToolsNeededWorksWithoutToolSupport(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("plain"))
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", ToolSupport: false}, "qwen-turbo",
		buildSubagentModelContext("", "", "hi"), nil, nil)

	require.NoError(t, err)
	require.Equal(t, "plain", out.Output)
	require.Empty(t, toolNames(rec.bodies[0]))
}

// The subagent message carries no images, but a text-only model's request must be
// rendered through the same vision gate as a chat turn.
func TestRunSubagentChatCompletions_TextOnlyModelNeverGetsImageParts(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("ok"))
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	mc := &provider.ModelContext{}
	mc.AppendUserMessage(provider.RoleUser, "look", []provider.UserMessageImage{{RawBytes: []byte{0x89, 0x50}, MediaType: "image/png"}}, false)

	_, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", VisionSupport: false}, "qwen3.7-plus", mc, nil, nil)
	require.NoError(t, err)

	raw, _ := json.Marshal(rec.bodies[0])
	require.NotContains(t, string(raw), "image_url")
}

func TestRunSubagentChatCompletions_MissingAPIKeyNamesTheEnvVar(t *testing.T) {
	t.Parallel()
	tests := []struct{ provider, model, env string }{
		{"qwen", "qwen3.7-plus", "QWEN_API_KEY"},
		{"mistral", "mistral-large-latest", "MISTRAL_API_KEY"},
		{"deepseek", "deepseek-chat", "DEEPSEEK_API_KEY"},
		{"xiaomi", "mimo-v2.5-pro", "XIAOMI_API_KEY"},
		{"google", "gemini-3.5-flash", "GEMINI_API_KEY"},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			a := &Agent{logger: zap.NewNop()}
			out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
				subagentModelCapabilities{Provider: tc.provider, ToolSupport: true}, tc.model,
				buildSubagentModelContext("", "", "hi"), nil, nil)
			require.Nil(t, out)
			require.ErrorContains(t, err, tc.env)
		})
	}
}

// Provider failures must keep the vendor's own message plus the provider label, so
// the caller can act on them.
func TestRunSubagentChatCompletions_ProviderErrorIsPreserved(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusBadRequest,
		`{"error":{"message":"model qwen3.7-plus does not accept parameter foo","type":"invalid_request_error","code":"invalid_parameter"}}`)
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", ToolSupport: true}, "qwen3.7-plus",
		buildSubagentModelContext("", "", "hi"), nil, nil)

	require.Nil(t, out)
	require.ErrorContains(t, err, "qwen subagent call failed")
	require.ErrorContains(t, err, "Qwen API call failed")
	require.ErrorContains(t, err, "does not accept parameter foo")
	require.ErrorContains(t, err, "400")
}

func TestRunSubagentChatCompletions_ContentPolicyBlockIsTypedAsSafetyViolation(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusBadRequest,
		`{"error":{"message":"Request was rejected by the safety system. safety_violations=[x]","code":"moderation_blocked"}}`)
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	_, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", ToolSupport: true}, "qwen3.7-plus",
		buildSubagentModelContext("", "", "hi"), nil, nil)

	violation, ok := provider.IsSafetyViolationError(err)
	require.True(t, ok)
	require.Equal(t, models.SafetyViolationProviderQwen, violation.Provider)
}

func TestRunSubagentChatCompletions_EmptyChoicesIsAnError(t *testing.T) {
	t.Parallel()
	rec := newChatCompletionsRecorder(t, http.StatusOK, `{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[]}`)
	a := newSubagentChatCompletionsAgent(rec.srv.URL)

	out, err := a.runSubagentChatCompletions(context.Background(), uuid.New(),
		subagentModelCapabilities{Provider: "qwen", ToolSupport: true}, "qwen3.7-plus",
		buildSubagentModelContext("", "", "hi"), nil, nil)

	require.Nil(t, out)
	require.ErrorContains(t, err, "qwen subagent call failed")
}

// expectModelRow queues the GetModelByName lookup callSubagentModel does to learn a
// model's provider and capabilities from its models-table row.
func expectModelRow(mock sqlmock.Sqlmock, name, providerName string, toolSupport bool) {
	cols := []string{"id", "name", "display_name", "description", "provider", "tool_support", "vision_support", "base_credits_per_slab", "subscription_tier", "deleted", "is_default"}
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows(cols).
		AddRow(uuid.New(), name, name, "", providerName, toolSupport, false, 1, "free", false, false))
}

// End to end through callSubagentModel: the model's DB row selects the provider, so a
// qwen row reaches the Qwen endpoint rather than the old rejection.
func TestCallSubagentModel_QwenRoutesToQwenProvider(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	expectModelRow(mock, "qwen3.7-plus", "qwen", true)

	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("qwen says hi"))
	a := newSubagentChatCompletionsAgent(rec.srv.URL)
	a.ds = ds

	out, err := a.callSubagentModel(context.Background(), uuid.New(), "qwen3.7-plus", buildSubagentModelContext("", "", "hi"), nil)

	require.NoError(t, err)
	require.Equal(t, "qwen says hi", out.Output)
	require.Equal(t, "Bearer qwen-key", rec.authHdrs[0])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCallSubagentModel_QwenWithoutKeyReportsConfigError(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	expectModelRow(mock, "qwen3.7-plus", "qwen", true)

	a := newTestAgent(ds)
	out, err := a.callSubagentModel(context.Background(), uuid.New(), "qwen3.7-plus", buildSubagentModelContext("", "", "hi"), nil)

	require.Nil(t, out)
	require.ErrorContains(t, err, "QWEN_API_KEY is not configured")
	require.NotContains(t, err.Error(), "not yet supported")
}

// z.ai shares the Anthropic Messages wire format; lock in that it keeps routing to
// its own client (a.ZAIProvider) and not the native Anthropic one.
func TestCallSubagentModel_ZAIRoutesToZAIProvider(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	expectModelRow(mock, "glm-5.2", "zai", true)

	var mu sync.Mutex
	var zaiHits, anthropicHits int
	zaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		zaiHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(claudeMessageTextJSONBody("msg_z", "glm says hi")))
	}))
	defer zaiSrv.Close()
	anthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		anthropicHits++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer anthropicSrv.Close()

	a := newTestAgent(ds)
	a.ZAIProvider = newHTTPTestClaudeProvider(zaiSrv.URL)
	a.ClaudeProvider = newHTTPTestClaudeProvider(anthropicSrv.URL)

	out, err := a.callSubagentModel(context.Background(), uuid.New(), "glm-5.2", buildSubagentModelContext("", "", "hi"), nil)

	require.NoError(t, err)
	require.Equal(t, "glm says hi", out.Output)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, zaiHits)
	require.Zero(t, anthropicHits)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubagentModelCapabilities_FallsBackToCatalogWithoutRow(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	caps := a.subagentModelCapabilities(context.Background(), "glm-5.2")
	require.Equal(t, "zai", caps.Provider)
	require.True(t, caps.ToolSupport)

	unknown := a.subagentModelCapabilities(context.Background(), "totally-unknown")
	require.Empty(t, unknown.Provider)
	require.True(t, unknown.ToolSupport, "no capability data keeps the previous (tools allowed) behaviour")
}

func TestErrSubagentToolsUnsupported_IsActionable(t *testing.T) {
	t.Parallel()
	err := errSubagentToolsUnsupported(models.ModelProviderQwen, "qwen-turbo", 3)
	require.ErrorContains(t, err, "qwen")
	require.ErrorContains(t, err, "qwen-turbo")
	require.ErrorContains(t, err, "3 MCP tool(s)")
	require.ErrorContains(t, err, "tool-capable model")
}
