package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

// recordingServer answers every request with body and keeps the last request's path and JSON.
func recordingServer(t *testing.T, body string) (*httptest.Server, *string, *map[string]any) {
	t.Helper()
	var path string
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &path, &req
}

var utilityTestRequest = provider.UtilityRequest{
	Role:         provider.UtilityRoleQuick,
	Instructions: "name it",
	Input:        "hello",
	Schema:       &provider.UtilitySchema{Name: "Name", Schema: map[string]any{"type": "object"}},
}

func TestCallUtility_QuickRoleUsesResponsesAPI(t *testing.T) {
	t.Parallel()
	srv, reqs := capturingResponsesServer(t, `{"id":"r","object":"response","status":"completed","output":[{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"output_text","text":"named","annotations":[]}]}]}`)
	client := openai.NewClient(option.WithAPIKey("k"), option.WithBaseURL(srv.URL))
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: provider.NewOpenAIProvider(nil, &client, nil, nil)}

	out, err := a.CallUtility(context.Background(), utilityTestRequest)
	require.NoError(t, err)
	require.Equal(t, "named", out)
	got := reqs()
	require.Len(t, got, 1)
	require.Equal(t, models.UtilityModelName, got[0]["model"])
	require.Equal(t, "json_schema", got[0]["text"].(map[string]any)["format"].(map[string]any)["type"])
}

func TestCallUtilityOn_RoutesAnthropicModelToMessagesAPI(t *testing.T) {
	t.Parallel()
	srv, path, req := recordingServer(t, `{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"named"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	a := &Agent{logger: zap.NewNop(), ClaudeProvider: provider.NewClaudeProviderWithBaseURL("k", srv.URL, nil, nil)}

	out, err := a.callUtilityOn(context.Background(), utilityTarget{name: "claude-haiku-4-5", provider: ""}, utilityTestRequest)
	require.NoError(t, err)
	require.Equal(t, "named", out.Text)
	require.Equal(t, "/v1/messages", *path)
	require.Equal(t, "claude-haiku-4-5", (*req)["model"])
	require.Contains(t, *req, "output_config")
}

func TestCallUtilityOn_RoutesChatCompletionsModel(t *testing.T) {
	t.Parallel()
	srv, path, req := recordingServer(t, `{"id":"c","object":"chat.completion","created":1,"model":"mistral-small-latest","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"named"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	a := &Agent{logger: zap.NewNop(), MistralProvider: provider.NewMistralProvider("k", srv.URL, nil, nil)}

	out, err := a.callUtilityOn(context.Background(), utilityTarget{name: "mistral-small-latest", provider: string(models.ModelProviderMistral)}, utilityTestRequest)
	require.NoError(t, err)
	require.Equal(t, "named", out.Text)
	require.Contains(t, *path, "/chat/completions")
	require.Contains(t, *req, "response_format")
	require.NotContains(t, *req, "tools", "utility calls offer no tools")
}

func TestCallUtilityOn_ReportsMissingProvider(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.callUtilityOn(context.Background(), utilityTarget{name: "mistral-small-latest", provider: string(models.ModelProviderMistral)}, utilityTestRequest)
	require.ErrorContains(t, err, "MISTRAL_API_KEY is not configured")
	_, err = a.callUtilityOn(context.Background(), utilityTarget{name: models.UtilityModelName, provider: ""}, utilityTestRequest)
	require.ErrorContains(t, err, "no OpenAI provider")
}

// A stored Responses conversation exists only on that API, so another model refuses to
// continue one rather than answering without the conversation.
func TestCallUtilityOn_PreviousResponseIDNeedsResponsesAPI(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	req := utilityTestRequest
	req.PreviousResponseID = "resp_1"
	_, err := a.callUtilityOn(context.Background(), utilityTarget{name: "claude-haiku-4-5", provider: ""}, req)
	require.ErrorContains(t, err, "cannot continue a stored Responses conversation")
}

// A failed call names the provider it went to, and keeps the cause for errors.Is.
func TestCallUtilityOn_ErrorNamesProvider(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	a := &Agent{
		logger:          zap.NewNop(),
		ClaudeProvider:  provider.NewClaudeProviderWithBaseURL("k", srv.URL, nil, nil),
		MistralProvider: provider.NewMistralProvider("k", srv.URL, nil, nil),
		OpenAIProvider:  newHTTPTestOpenAIProvider(srv.URL),
	}
	for _, tt := range []struct{ model, modelProvider, name string }{
		{"claude-haiku-4-5", "", "Anthropic (claude-haiku-4-5):"},
		{"mistral-small-latest", string(models.ModelProviderMistral), "Mistral (mistral-small-latest):"},
		{models.UtilityModelName, "", "OpenAI (" + models.UtilityModelName + "):"},
	} {
		_, err := a.callUtilityOn(context.Background(), utilityTarget{name: tt.model, provider: tt.modelProvider}, utilityTestRequest)
		require.Error(t, err, tt.model)
		require.True(t, strings.HasPrefix(err.Error(), tt.name), "%s: %v", tt.model, err)
	}
}

func TestCallUtilityOn_RefusesStoredResponseWithConversation(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	req := utilityTestRequest
	req.PreviousResponseID = "resp_1"
	req.Context = &provider.ModelContext{}
	_, err := a.callUtilityOn(context.Background(), utilityTarget{name: models.UtilityModelName, provider: ""}, req)
	require.ErrorContains(t, err, "not both")
}

// Every role's model must resolve to an API from its name alone, or every call on that role
// fails; renaming one to a model outside the catalog would otherwise surface only at runtime.
func TestUtilityModelName_EveryRoleResolvesToResponsesAPI(t *testing.T) {
	t.Parallel()
	for _, role := range []provider.UtilityRole{provider.UtilityRoleQuick, provider.UtilityRoleMemory, provider.UtilityRolePersonality} {
		model := utilityModelName(role)
		require.Equal(t, models.ModelProviderOpenAI, models.ProviderForModel("", model), "%s -> %s", role, model)
		require.False(t, models.UsesOpenAIChatCompletionsAPI("", model), model)
	}
	require.True(t, models.IsNativeAnthropicModel("", archivalClaudeModel), archivalClaudeModel)
}

// A conversation sent to the Responses API loses the images it can't take, and the call goes
// ahead; each drop is counted by reason so it shows up rather than vanishing.
func TestCallUtility_CountsImagesLeftOut(t *testing.T) {
	t.Parallel()
	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", "summary"))
	tm := telemetrytest.New(t)
	a := &Agent{
		logger:         zap.NewNop(),
		telemetry:      &telemetry.Telemetry{Logger: zap.NewNop(), Metrics: tm.Metrics},
		OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL),
	}
	mc := &provider.ModelContext{}
	mc.AppendHistoryTurn(provider.RoleUser, "photos", []provider.UserMessageImage{
		{MediaType: "image/jpeg", RawBytes: []byte{1}},
		{MediaType: "image/heic", RawBytes: []byte{2}},
		{MediaType: "image/png"},
	}, false)

	out, err := a.CallUtility(context.Background(), provider.UtilityRequest{
		Role: provider.UtilityRoleMemory, CallPath: telemetry.CallPathConversationSummary, Context: mc,
	})
	require.NoError(t, err)
	require.Equal(t, "summary", out)
	require.Len(t, reqs(), 1)
	for reason, want := range map[string]int64{"unsupported_type": 1, "no_data": 1} {
		require.Equal(t, want, tm.CounterValue(t, telemetry.GenAIImagesDropped.Name,
			telemetry.AttrGenAIProvider.String(string(models.ModelProviderOpenAI)),
			telemetry.AttrCallPath.String(string(telemetry.CallPathConversationSummary)),
			telemetry.AttrReason.String(reason)), reason)
	}
}

// GLM always reasons, so a utility call on it bounds the reasoning low and raises the cap, or
// the reasoning can use the whole cap and leave no answer.
func TestCallUtilityOn_ZAIBoundsReasoning(t *testing.T) {
	t.Parallel()
	srv, path, req := recordingServer(t, `{"id":"m","type":"message","role":"assistant","model":"glm-5","content":[{"type":"text","text":"named"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	a := &Agent{logger: zap.NewNop(), ZAIProvider: provider.NewClaudeProviderWithBaseURL("k", srv.URL, nil, nil)}

	out, err := a.callUtilityOn(context.Background(), utilityTarget{name: "glm-5", provider: string(models.ModelProviderZAI)}, utilityTestRequest)
	require.NoError(t, err)
	require.Equal(t, "named", out.Text)
	require.Equal(t, "/v1/messages", *path)
	cfg := (*req)["output_config"].(map[string]any)
	require.Equal(t, "low", cfg["effort"])
	require.Equal(t, "json_schema", cfg["format"].(map[string]any)["type"], "the schema survives")
	require.EqualValues(t, provider.ReasoningMaxOutputTokens, (*req)["max_tokens"])
}
