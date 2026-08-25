package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ AgentAdapter = (*MockAdapter)(nil)

func TestSplitStreamDeltas_ConcatenationEqualsInput(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"simple sentence", "Hello world, this is a test."},
		{"no whitespace", "abcdefghijklmnopqrstuvwxyz0123456789"},
		{"leading and trailing whitespace", "  padded text  "},
		{"newline heavy", "line one\n\nline two\n\tindented\nline three\n"},
		{"punctuation", "Wait... what?! (Really; truly) — yes: \"quoted\"."},
		{"multi-byte unicode", "héllo wörld — 日本語のテキスト and émojis 🎉🚀 mixed"},
		{"unicode no-space", "日本語テキストだけ"},
		{"single char", "x"},
		{"only whitespace", " \n\t "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deltas := SplitStreamDeltas(tt.text)
			assert.Equal(t, tt.text, strings.Join(deltas, ""), "deltas must concatenate to the input")
			for _, d := range deltas {
				assert.NotEmpty(t, d)
			}
		})
	}

	assert.Nil(t, SplitStreamDeltas(""))
	assert.Greater(t, len(SplitStreamDeltas("one two three")), 1, "whitespace-separated text streams as multiple deltas")
}

func TestMockAdapter_EchoStreamsAndReturnsUserTurn(t *testing.T) {
	const userTurn = "Please echo this — 日本語も ok?\nSecond line."
	adapter := NewMockAdapter(MockAdapterConfig{Mode: MockModeEcho, EchoText: userTurn})

	var streamed strings.Builder
	deltaCount := 0
	adapter.SetTextDeltaHandler(func(delta string) {
		streamed.WriteString(delta)
		deltaCount++
	})

	resp, toolUses, err := adapter.Call(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, toolUses)
	assert.Equal(t, userTurn, resp.Text)
	assert.Equal(t, resp.Text, streamed.String(), "draft (streamed deltas) must equal final text")
	assert.Greater(t, deltaCount, 1)
	assert.NotEmpty(t, resp.ID)
	assert.Greater(t, resp.OutputTokens, int64(0))
}

func TestMockAdapter_EmptyEchoFallsBack(t *testing.T) {
	adapter := NewMockAdapter(MockAdapterConfig{Mode: MockModeEcho})
	resp, _, err := adapter.Call(context.Background())
	require.NoError(t, err)
	assert.Equal(t, mockEmptyEchoFallback, resp.Text)
}

func TestMockAdapter_FixedModeCycles(t *testing.T) {
	adapter := NewMockAdapter(MockAdapterConfig{
		Mode:           MockModeFixed,
		FixedResponses: []string{"first", "second"},
	})
	for _, want := range []string{"first", "second", "first"} {
		resp, _, err := adapter.Call(context.Background())
		require.NoError(t, err)
		assert.Equal(t, want, resp.Text)
	}

	empty := NewMockAdapter(MockAdapterConfig{Mode: MockModeFixed})
	resp, _, err := empty.Call(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text)
}

func TestMockAdapter_ScriptedToolTurnThenFinal(t *testing.T) {
	adapter := NewMockAdapter(MockAdapterConfig{
		Mode: MockModeScripted,
		Script: []MockScriptStep{
			{ToolUses: []ToolUse{{ID: "t1", Name: "some_tool", Input: []byte(`{"x":1}`)}}},
			{Text: "final scripted answer"},
		},
	})

	resp, toolUses, err := adapter.Call(context.Background())
	require.NoError(t, err)
	assert.Nil(t, resp)
	require.Len(t, toolUses, 1)
	assert.Equal(t, "some_tool", toolUses[0].Name)

	adapter.AppendToolResults([]ToolResult{{ID: "t1", Output: "ok"}})
	require.Len(t, adapter.AppendedBatches(), 1)

	resp, toolUses, err = adapter.Call(context.Background())
	require.NoError(t, err)
	assert.Empty(t, toolUses)
	assert.Equal(t, "final scripted answer", resp.Text)

	_, _, err = adapter.Call(context.Background())
	assert.Error(t, err, "exhausted script must error rather than loop forever")
}

func TestMockAdapter_HonorsContextCancellation(t *testing.T) {
	adapter := NewMockAdapter(MockAdapterConfig{
		Mode:     MockModeEcho,
		EchoText: "one two three four five six seven eight",
	})
	ctx, cancel := context.WithCancel(context.Background())
	deltas := 0
	adapter.SetTextDeltaHandler(func(string) {
		deltas++
		if deltas == 2 {
			cancel()
		}
	})

	resp, _, err := adapter.Call(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, resp)
	assert.Equal(t, 2, deltas, "streaming must stop mid-stream on cancellation")
}

func TestMockAdapter_ForceFinalResponseStreams(t *testing.T) {
	adapter := NewMockAdapter(MockAdapterConfig{Mode: MockModeEcho, EchoText: "hi"})
	var streamed strings.Builder
	adapter.SetTextDeltaHandler(func(delta string) { streamed.WriteString(delta) })

	resp, err := adapter.ForceFinalResponse(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text)
	assert.Equal(t, resp.Text, streamed.String())
}
