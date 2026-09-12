package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func writeResponsesSSE(t *testing.T, w http.ResponseWriter, frame string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := w.(http.Flusher)
	require.True(t, ok, "test server response writer must support flushing")
	_, _ = w.Write([]byte("data: " + frame + "\n\n"))
	flusher.Flush()
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func TestHandleAgentLoop_OpenAIIncompleteToolArgumentsRecoverAndContinue(t *testing.T) {
	var requestCount atomic.Int32
	var sawPreviousResponseID atomic.Bool
	var sawMatchingToolOutput atomic.Bool

	incompleteResponse := `{"id":"resp_incomplete","object":"response","created_at":1,"model":"test","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"update_scratchpad","status":"incomplete","arguments":"{\"operation\":\"replace\",\"content\":\"partial"}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`
	incompleteEvent := `{"type":"response.incomplete","sequence_number":1,"response":` + incompleteResponse + `}`
	completedResponse := `{"id":"resp_final","object":"response","created_at":2,"model":"test","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"recovered after tool failure"}]}],"usage":{"input_tokens":6,"output_tokens":5,"total_tokens":11}}`
	completedEvent := `{"type":"response.completed","sequence_number":2,"response":` + completedResponse + `}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		switch requestCount.Add(1) {
		case 1:
			writeResponsesSSE(t, w, incompleteEvent)
		case 2:
			var request map[string]any
			require.NoError(t, json.Unmarshal(body, &request))
			if request["previous_response_id"] == "resp_incomplete" {
				sawPreviousResponseID.Store(true)
			}
			if input, ok := request["input"].([]any); ok {
				for _, raw := range input {
					item, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					if item["type"] == "function_call_output" && item["call_id"] == "call_1" {
						sawMatchingToolOutput.Store(true)
					}
				}
			}
			writeResponsesSSE(t, w, completedEvent)
		default:
			t.Fatalf("unexpected OpenAI request %d", requestCount.Load())
		}
	}))
	defer srv.Close()

	client := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(srv.URL),
	)
	openAIProvider := provider.NewOpenAIProvider(nil, &client, nil, nil)
	adapter := provider.NewOpenAIAdapter(openAIProvider, responses.ResponseNewParams{Model: "test"})
	// Production chat turns install a text-delta handler, which selects the streaming path.
	adapter.SetTextDeltaHandler(func(string) {})

	logger := zap.NewNop()
	a := &Agent{
		logger:         logger,
		scratchpadTool: tools.NewScratchpadTool(nil, logger),
	}
	chatCtx := &chatContext{chat: &models.Chat{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		PersonalityID: uuid.New(),
	}}

	result, toolCalls, generatedAttachments, err := a.handleAgentLoop(t.Context(), chatCtx, adapter)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "recovered after tool failure", result.Text)
	require.Empty(t, generatedAttachments)
	require.Len(t, toolCalls, 1)
	require.Equal(t, "update_scratchpad", toolCalls[0].ToolName)
	require.Contains(t, toolCalls[0].ToolError, "invalid arguments")
	require.True(t, sawPreviousResponseID.Load(), "follow-up request must thread the incomplete response id")
	require.True(t, sawMatchingToolOutput.Load(), "follow-up request must include a result for the incomplete tool call id")
	require.Equal(t, int32(2), requestCount.Load())
}
