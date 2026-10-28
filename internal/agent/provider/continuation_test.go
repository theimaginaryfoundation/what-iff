package provider

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeFinalMessage(t *testing.T) *anthropic.Message {
	t.Helper()
	var msg anthropic.Message
	require.NoError(t, json.Unmarshal([]byte(`{"id":"m1","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"end_turn",
		"content":[{"type":"thinking","thinking":"weighing it","signature":"sig-1"},{"type":"text","text":"interim answer"}],
		"usage":{"input_tokens":10,"output_tokens":5}}`), &msg))
	return &msg
}

// ContinueWith records the final reply (thinking kept when replay is on, as the API requires
// with thinking enabled) and then the continuation as a user turn.
func TestClaudeAdapter_ContinueWithRecordsReplyThenUser(t *testing.T) {
	for _, replay := range []bool{true, false} {
		a := NewClaudeAdapter(nil, anthropic.MessageNewParams{Model: "claude-opus-5"}, nil, false, nil, nil)
		a.SetReplayThinking(replay)
		a.lastFinal = claudeFinalMessage(t)

		a.ContinueWith("keep going")

		require.Len(t, a.params.Messages, 2)
		assistant, user := a.params.Messages[0], a.params.Messages[1]
		assert.Equal(t, anthropic.MessageParamRoleAssistant, assistant.Role)
		assert.Equal(t, anthropic.MessageParamRoleUser, user.Role)
		raw, err := json.Marshal(assistant)
		require.NoError(t, err)
		assert.Contains(t, string(raw), "interim answer")
		if replay {
			assert.Contains(t, string(raw), `"signature":"sig-1"`)
		} else {
			assert.NotContains(t, string(raw), "thinking")
		}
		assert.Nil(t, a.lastFinal)
	}
}

// The OpenAI server already holds the last reply (previous_response_id), so only the user turn
// is sent.
func TestOpenAIAdapter_ContinueWithSendsOnlyTheUserTurn(t *testing.T) {
	a := NewOpenAIAdapter(nil, responses.ResponseNewParams{Model: "gpt-5.1"})
	a.previousResponseID = "resp_1"
	a.ContinueWith("keep going")
	require.Len(t, a.params.Input.OfInputItemList, 1)
	raw, err := json.Marshal(a.params.Input.OfInputItemList[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), "keep going")
	assert.Contains(t, string(raw), `"role":"user"`)
}

// On the beta MCP path, ContinueWith records the last beta reply then the user turn.
func TestClaudeAdapter_ContinueWithBetaPath(t *testing.T) {
	a := NewClaudeAdapter(nil, anthropic.MessageNewParams{Model: "claude-opus-5"}, nil, false, nil, nil)
	a.useBetaMCP = true
	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(`{"id":"b1","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"end_turn","content":[{"type":"text","text":"beta answer"}],"usage":{"input_tokens":3,"output_tokens":2}}`), &msg))
	a.lastFinalBeta = &msg

	a.ContinueWith("keep going")
	require.Len(t, a.betaParams.Messages, 2)
	raw, err := json.Marshal(a.betaParams.Messages)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "beta answer")
	assert.Contains(t, string(raw), "keep going")
	assert.Nil(t, a.lastFinalBeta)

	// Without a remembered reply only the user turn is added.
	a.ContinueWith("again")
	assert.Len(t, a.betaParams.Messages, 3)
}

// A Mistral reply with thinking is recorded as a thinking chunk plus text before a continuation;
// one without thinking stays plain text.
func TestMistralChunkReplay_FinalTurn(t *testing.T) {
	raw, err := json.Marshal(mistralChunkReplay{}.finalTurn("the answer", "my thinking"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"type":"thinking"`)
	assert.Contains(t, string(raw), `"text":"the answer"`)

	raw, err = json.Marshal(mistralChunkReplay{}.finalTurn("plain", ""))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"content":"plain"`)
}
