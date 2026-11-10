package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockDirectives_DriveToolRoundsThenAnswer(t *testing.T) {
	m := NewMockAdapter(MockAdapterConfig{Mode: MockModeEcho, EchoText: "Checking with you.\n/mock-tool ask_user {\"approval\":{\"action\":\"go\"}}\n/mock-wait 10ms\n/mock-tool other not-json"})

	_, uses, err := m.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, uses, 1)
	assert.Equal(t, "ask_user", uses[0].Name)
	assert.JSONEq(t, `{"approval":{"action":"go"}}`, string(uses[0].Input))
	m.AppendToolResults([]ToolResult{{ID: uses[0].ID, Output: `{"approved":true}`}})

	start := time.Now()
	_, uses, err = m.Call(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 10*time.Millisecond)
	require.Len(t, uses, 1)
	assert.Equal(t, "other", uses[0].Name)
	assert.JSONEq(t, `{}`, string(uses[0].Input), "invalid JSON arguments become {}")

	res, uses, err := m.Call(context.Background())
	require.NoError(t, err)
	assert.Empty(t, uses)
	assert.Contains(t, res.Text, "Checking with you.")
	assert.Contains(t, res.Text, `mock_call_1 (ok): {"approved":true}`)
}

func TestMockDirectives_PlainEchoUnchanged(t *testing.T) {
	m := NewMockAdapter(MockAdapterConfig{Mode: MockModeEcho, EchoText: "hello /mock-tool inline is not a directive"})
	res, uses, err := m.Call(context.Background())
	require.NoError(t, err)
	assert.Empty(t, uses)
	assert.Equal(t, "hello /mock-tool inline is not a directive", res.Text)
}
