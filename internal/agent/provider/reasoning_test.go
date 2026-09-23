package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

// Reasoning capture and live streaming for the always-on reasoning providers: z.ai
// GLM (thinking blocks on the Claude path) and Xiaomi MiMo (reasoning_content on Chat
// Completions). Shared fixtures live in truncation_fallback_test.go.

func claudeMessageThinkingToolUseJSON(id, thinking, toolID, name, input string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"thinking","thinking":"` + thinking + `","signature":"sig"},` +
		`{"type":"tool_use","id":"` + toolID + `","name":"` + name + `","input":` + input + `}],` +
		`"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":7}}`
}

func TestClaudeAdapter_ReasoningAccumulatesAcrossToolRounds(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{
		claudeMessageThinkingToolUseJSON("msg_1", "first I should look it up", "toolu_1", "lookup", `{}`),
		claudeMessageThinkingTextJSON("msg_2", "now I can answer", "the answer"),
	})
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	resp, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Len(t, uses, 1)
	a.AppendToolResults([]ToolResult{{ID: "toolu_1", Output: "found"}})

	resp, uses, err = a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, uses)
	require.Equal(t, "the answer", resp.Text)
	require.Equal(t, "first I should look it up\n\nnow I can answer", resp.Reasoning)
}

func TestXiaomiAdapter_CapturesReasoningNonStreaming(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{chatCompletionReasoningTextJSON("c1", "17*23 is 391", "391", "stop")})
	defer srv.Close()

	resp, _, err := newTestXiaomiAdapter(srv.URL).Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "391", resp.Text)
	require.Equal(t, "17*23 is 391", resp.Reasoning)
	require.Equal(t, "stop", resp.StopReason)
}

func TestXiaomiAdapter_CapturesReasoningStreaming(t *testing.T) {
	reasoningFrame := func(r string) string {
		return `{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"test",` +
			`"choices":[{"index":0,"delta":{"reasoning_content":"` + r + `"},"finish_reason":null}]}`
	}
	srv := sseServer(t, []string{reasoningFrame("let me "), reasoningFrame("think"), deltaFrame("hi"), deltaFrame(" there")})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	var streamed string
	a.SetTextDeltaHandler(func(d string) { streamed += d })
	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "hi there", streamed, "reasoning must not leak into the text stream")
	require.Equal(t, "let me think", resp.Reasoning)
}

// liveRecorder captures a ReasoningStream as the consumer (the job draft) sees it.
type liveRecorder struct {
	draft  string
	resets int
}

func (r *liveRecorder) stream() ReasoningStream {
	return ReasoningStream{
		OnDelta: func(d string) { r.draft += d },
		OnReset: func() { r.draft = ""; r.resets++ },
	}
}

func TestReasoningRelay_SeparatesRoundsAndResetKeepsEarlierRounds(t *testing.T) {
	var kept reasoningLog
	var rec liveRecorder
	relay := reasoningRelay{stream: rec.stream(), kept: &kept}

	relay.beginCall()
	relay.delta("round one")
	kept.add("round one")

	relay.beginCall()
	relay.delta("round two, ")
	relay.delta("abandoned")
	require.Equal(t, "round one\n\nround two, abandoned", rec.draft)

	// The second call is discarded: the draft rolls back to the kept rounds.
	relay.reset()
	require.Equal(t, 1, rec.resets)
	require.Equal(t, "round one", rec.draft)

	relay.beginCall()
	relay.delta("retry")
	kept.add("retry")
	require.Equal(t, "round one\n\nretry", rec.draft)
	require.Equal(t, kept.String(), rec.draft, "live draft ends up matching the saved reasoning")

	// Reset with nothing streamed for the current call is a no-op.
	relay.beginCall()
	relay.reset()
	require.Equal(t, 1, rec.resets)
}

func TestReasoningRelay_DisabledIsSafe(t *testing.T) {
	var relay reasoningRelay
	require.False(t, relay.enabled())
	require.NotPanics(t, func() {
		relay.beginCall()
		relay.delta("x")
		relay.reset()
	})
}

// rawSSESequenceServer streams bodies[N] (raw SSE text) for request N, clamped to the last.
func rawSSESequenceServer(t *testing.T, bodies []string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	n := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		idx := n
		n++
		mu.Unlock()
		if idx >= len(bodies) {
			idx = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(bodies[idx]))
	}))
}

func claudeSSE(id string, thinking []string, text []string, stopReason string) string {
	var b strings.Builder
	ev := func(event, data string) { b.WriteString("event: " + event + "\ndata: " + data + "\n\n") }
	ev("message_start", `{"type":"message_start","message":{"id":"`+id+`","type":"message","role":"assistant","model":"test-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`)
	idx := 0
	if len(thinking) > 0 {
		ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`)
		for _, t := range thinking {
			ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"`+t+`"}}`)
		}
		ev("content_block_stop", `{"type":"content_block_stop","index":0}`)
		idx++
	}
	if len(text) > 0 {
		i := strconv.Itoa(idx)
		ev("content_block_start", `{"type":"content_block_start","index":`+i+`,"content_block":{"type":"text","text":""}}`)
		for _, t := range text {
			ev("content_block_delta", `{"type":"content_block_delta","index":`+i+`,"delta":{"type":"text_delta","text":"`+t+`"}}`)
		}
		ev("content_block_stop", `{"type":"content_block_stop","index":`+i+`}`)
	}
	ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+stopReason+`","stop_sequence":null},"usage":{"output_tokens":5}}`)
	ev("message_stop", `{"type":"message_stop"}`)
	return b.String()
}

func TestClaudeAdapter_StreamsThinkingLiveAndResetsOnTruncationFallback(t *testing.T) {
	srv := rawSSESequenceServer(t, []string{
		claudeSSE("msg_trunc", []string{"thinking ", "forever"}, nil, "max_tokens"),
		claudeSSE("msg_ok", []string{"brief ", "thought"}, []string{"Hi", " there"}, "end_turn"),
	})
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	a.SetTruncationFallback(func(*anthropic.MessageNewParams) {})
	var rec liveRecorder
	a.SetReasoningStream(rec.stream())
	var text string
	a.SetTextDeltaHandler(func(d string) { text += d })

	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Hi there", text, "thinking never leaks into the text stream")
	require.Equal(t, 1, rec.resets, "the truncated attempt's live thinking is discarded")
	require.Equal(t, "brief thought", rec.draft)
	require.Equal(t, "brief thought", resp.Reasoning)
}

// A transient failure mid-thinking is retried by callClaudeWithRetry (only streamed
// *text* blocks a retry), so the next attempt must reset the live draft first.
func TestRetryAwareThinking_ResetsOnlyAfterStreamedAttempt(t *testing.T) {
	var rec liveRecorder
	onThinking, beforeAttempt := retryAwareThinking(rec.stream())

	beforeAttempt() // first attempt: nothing to reset
	onThinking("half a thought")
	beforeAttempt() // retry after thinking streamed
	require.Equal(t, 1, rec.resets)
	require.Empty(t, rec.draft)

	beforeAttempt() // retry after an attempt that streamed nothing
	require.Equal(t, 1, rec.resets)
	onThinking("whole thought")
	require.Equal(t, "whole thought", rec.draft)

	onThinking, beforeAttempt = retryAwareThinking(ReasoningStream{})
	require.Nil(t, onThinking, "no listener means no thinking hook")
	require.NotPanics(t, beforeAttempt)
}

func chatCompletionSSE(reasoning []string, content []string, finish string) string {
	var b strings.Builder
	frame := func(delta string, finishReason string) {
		fr := "null"
		if finishReason != "" {
			fr = `"` + finishReason + `"`
		}
		b.WriteString(`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + fr + `}]}` + "\n\n")
	}
	for _, r := range reasoning {
		frame(`{"reasoning_content":"`+r+`"}`, "")
	}
	for _, c := range content {
		frame(`{"content":"`+c+`"}`, "")
	}
	frame(`{}`, finish)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestXiaomiAdapter_StreamsReasoningLiveAndResetsOnTruncation(t *testing.T) {
	srv := rawSSESequenceServer(t, []string{
		chatCompletionSSE([]string{"thinking ", "forever"}, nil, "length"),
		chatCompletionSSE(nil, []string{"answer"}, "stop"),
	})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	var rec liveRecorder
	a.SetReasoningStream(rec.stream())
	var text string
	a.SetTextDeltaHandler(func(d string) { text += d })

	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "answer", text)
	require.Equal(t, 1, rec.resets)
	require.Empty(t, rec.draft, "thinking was disabled for the retry, so nothing is kept")
	require.Empty(t, resp.Reasoning)
}

func TestXiaomiAdapter_LiveReasoningSeparatesToolRounds(t *testing.T) {
	toolRound := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"reasoning_content":"need a tool"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
	srv := rawSSESequenceServer(t, []string{toolRound, chatCompletionSSE([]string{"got it"}, []string{"done"}, "stop")})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	var rec liveRecorder
	a.SetReasoningStream(rec.stream())
	a.SetTextDeltaHandler(func(string) {})

	_, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, uses, 1)
	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "x"}})
	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "need a tool\n\ngot it", rec.draft)
	require.Equal(t, resp.Reasoning, rec.draft)
}
