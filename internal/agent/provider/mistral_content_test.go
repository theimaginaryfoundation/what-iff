package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func mistralFrame(delta, finish string) string {
	return `data: {"id":"m1","object":"chat.completion.chunk","created":1,"model":"mistral-large-4","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + finish + `}]}` + "\n\n"
}

// A reasoning stream goes through three shapes: thinking chunks, one chunk array carrying the end
// of the thinking and the first reply text, then plain strings. Thinking reaches the reasoning
// stream, text the reply, and no raw JSON leaks into either.
func TestMistralAdapter_StreamSplitsThinkingChunksFromText(t *testing.T) {
	sse := mistralFrame(`{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"Let me "}]}]}`, "null") +
		mistralFrame(`{"content":[{"type":"thinking","thinking":[{"type":"text","text":"think."}]},{"type":"text","text":"The answer"}]}`, "null") +
		mistralFrame(`{"content":" is 4."}`, "null") +
		mistralFrame(`{}`, `"stop"`) + "data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	a := NewMistralAdapter(NewMistralProvider("k", srv.URL, nil, nil), openai.ChatCompletionNewParams{Model: "mistral-large-4"}, nil, nil)
	var text, live strings.Builder
	a.SetTextDeltaHandler(func(d string) { text.WriteString(d) })
	a.SetReasoningStream(ReasoningStream{OnDelta: func(d string) { live.WriteString(d) }})

	resp, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, uses)
	require.Equal(t, "The answer is 4.", text.String())
	require.Equal(t, "The answer is 4.", resp.Text)
	require.Equal(t, "Let me think.", live.String())
	require.Equal(t, "Let me think.", resp.Reasoning)
}

func TestMistralContent_NonStreamedChunksAndPlainStrings(t *testing.T) {
	text, thinking := mistralContent(gjson.Parse(`[{"type":"thinking","thinking":[{"type":"text","text":"hmm"}]},{"type":"text","text":"hi"}]`))
	require.Equal(t, "hi", text)
	require.Equal(t, "hmm", thinking)
	text, thinking = mistralContent(gjson.Parse(`"plain"`))
	require.Equal(t, "plain", text)
	require.Empty(t, thinking)
}

// A tool round's thinking is replayed as a thinking chunk at the head of the assistant content.
func TestMistralAdapter_ReplaysThinkingChunkOnToolCall(t *testing.T) {
	tool := mistralFrame(`{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"need weather"}]}]}`, "null") +
		mistralFrame(`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`, "null") +
		mistralFrame(`{}`, `"tool_calls"`) + "data: [DONE]\n\n"
	srv, request := recordingSSEServer(t, []string{tool, chatCompletionSSE(nil, []string{"Sunny."}, "stop")})
	defer srv.Close()

	a := NewMistralAdapter(NewMistralProvider("k", srv.URL, nil, nil), openai.ChatCompletionNewParams{Model: "mistral-large-4"}, nil, nil)
	a.SetTextDeltaHandler(func(string) {})
	_, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, uses, 1)
	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "sunny"}})
	_, _, err = a.Call(context.Background())
	require.NoError(t, err)

	assistant := gjson.Get(request(1), `messages.#(role=="assistant")`)
	require.Equal(t, "thinking", assistant.Get("content.0.type").String(), request(1))
	require.Equal(t, "need weather", assistant.Get("content.0.thinking.0.text").String())
	require.Equal(t, "call_1", assistant.Get("tool_calls.0.id").String())
	require.False(t, assistant.Get("reasoning_content").Exists())
}
