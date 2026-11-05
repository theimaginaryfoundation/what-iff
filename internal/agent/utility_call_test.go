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

	out, err := a.callUtilityOn(context.Background(), "claude-haiku-4-5", "", utilityTestRequest)
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

	out, err := a.callUtilityOn(context.Background(), "mistral-small-latest", string(models.ModelProviderMistral), utilityTestRequest)
	require.NoError(t, err)
	require.Equal(t, "named", out.Text)
	require.Contains(t, *path, "/chat/completions")
	require.Contains(t, *req, "response_format")
	require.NotContains(t, *req, "tools", "utility calls offer no tools")
}

func TestCallUtilityOn_ReportsMissingProvider(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.callUtilityOn(context.Background(), "mistral-small-latest", string(models.ModelProviderMistral), utilityTestRequest)
	require.ErrorContains(t, err, "MISTRAL_API_KEY is not configured")
	_, err = a.callUtilityOn(context.Background(), models.UtilityModelName, "", utilityTestRequest)
	require.ErrorContains(t, err, "no OpenAI provider")
}

// A stored Responses conversation exists only on that API, so another model refuses to
// continue one rather than answering without the conversation.
func TestCallUtilityOn_PreviousResponseIDNeedsResponsesAPI(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	req := utilityTestRequest
	req.PreviousResponseID = "resp_1"
	_, err := a.callUtilityOn(context.Background(), "claude-haiku-4-5", "", req)
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
		{"claude-haiku-4-5", "", "Anthropic:"},
		{"mistral-small-latest", string(models.ModelProviderMistral), "Mistral:"},
		{models.UtilityModelName, "", "OpenAI:"},
	} {
		_, err := a.callUtilityOn(context.Background(), tt.model, tt.modelProvider, utilityTestRequest)
		require.Error(t, err, tt.model)
		require.True(t, strings.HasPrefix(err.Error(), tt.name), "%s: %v", tt.model, err)
	}
}
