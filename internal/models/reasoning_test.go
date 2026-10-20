package models

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func effortPtr(e ReasoningEffort) *ReasoningEffort { return &e }
func loopPtr(l AgentLoop) *AgentLoop               { return &l }

func TestResolveTurnSettings_Precedence(t *testing.T) {
	thread := TurnSettingLevel{ReasoningEffort: effortPtr(ReasoningEffortLow), AgentLoop: loopPtr(AgentLoopStandard)}
	mode := TurnSettingLevel{ReasoningEffort: effortPtr(ReasoningEffortHigh)}
	job := TurnSettingLevel{AgentLoop: loopPtr(AgentLoopAdvanced)}

	got := ResolveTurnSettings(thread, mode, job)
	assert.Equal(t, ReasoningEffortHigh, got.ReasoningEffort, "mode overrides thread")
	assert.Equal(t, SettingSourceMode, got.ReasoningEffortSource)
	assert.Equal(t, AgentLoopAdvanced, got.AgentLoop, "job overrides everything")
	assert.Equal(t, SettingSourceJob, got.AgentLoopSource)
	assert.True(t, got.Advanced())

	got = ResolveTurnSettings(thread, TurnSettingLevel{}, TurnSettingLevel{})
	assert.Equal(t, ReasoningEffortLow, got.ReasoningEffort)
	assert.Equal(t, SettingSourceThread, got.ReasoningEffortSource)

	got = ResolveTurnSettings(TurnSettingLevel{}, TurnSettingLevel{}, TurnSettingLevel{})
	assert.Equal(t, TurnSettings{}, got, "nothing set anywhere is today's behaviour")
	assert.False(t, got.Advanced())

	bad := ReasoningEffort("extreme")
	got = ResolveTurnSettings(thread, TurnSettingLevel{ReasoningEffort: &bad}, TurnSettingLevel{})
	assert.Equal(t, ReasoningEffortLow, got.ReasoningEffort, "invalid stored values are skipped")
}

func TestReasoningSupportFor(t *testing.T) {
	cases := []struct {
		provider, name string
		want           ReasoningSupport
	}{
		{"openai", "gpt-5.1", ReasoningSupportOptional},
		{"openai", "gpt-5-mini", ReasoningSupportOptional},
		{"openai", "gpt-4o", ReasoningSupportNone},
		{"anthropic", "claude-opus-5", ReasoningSupportOptional},
		{"anthropic", "claude-haiku-4-5", ReasoningSupportOptional},
		{"anthropic", "claude-opus-5-5", ReasoningSupportAlways},
		{"anthropic", "claude-fable-5-1", ReasoningSupportAlways},
		{"anthropic", "claude-3-5-haiku", ReasoningSupportNone},
		{"zai", "glm-5.3", ReasoningSupportAlways},
		{"xiaomi", "mimo-v2", ReasoningSupportToggle},
		{"google", "gemini-3.8-flash", ReasoningSupportAlways},
		{"google", "gemini-2.5-flash", ReasoningSupportOptional},
		{"google", "gemini-2.5-pro", ReasoningSupportAlways},
		{"google", "gemini-nano-banana-2.1", ReasoningSupportNone},
		{"qwen", "qwen3.7-plus", ReasoningSupportOptional},
		{"qwen", "qwen-plus", ReasoningSupportOptional},
		{"qwen", "qwen3.7-max-preview", ReasoningSupportAlways},
		{"qwen", "qwen-turbo", ReasoningSupportNone},
		{"qwen", "qwen-image-2.0", ReasoningSupportNone},
		{"deepseek", "deepseek-flash", ReasoningSupportOptional},
		{"deepseek", "deepseek-v4-pro", ReasoningSupportOptional},
		{"mistral", "mistral-large-4", ReasoningSupportToggle},
		{"mistral", "mistral-small-latest", ReasoningSupportNone}, // aliases are not trusted
		// Fail closed: models the tables do not list get no reasoning control.
		{"openai", "o3", ReasoningSupportAlways},
		{"openai", "o4-mini", ReasoningSupportAlways},
		{"openai", "o1-mini", ReasoningSupportNone},
		{"openai", "gpt-5-chat-latest", ReasoningSupportNone},
		{"openai", "gpt-5-pro", ReasoningSupportNone},
		{"openai", "gpt-4.1-mini", ReasoningSupportNone},
		{"anthropic", "claude-sonnet-4-5", ReasoningSupportOptional},
		{"anthropic", "claude-opus-4-1", ReasoningSupportOptional},
		{"anthropic", "claude-sonnet-4-6", ReasoningSupportOptional},
		{"anthropic", "claude-future-9", ReasoningSupportNone},
		{"google", "gemini-1.5-pro", ReasoningSupportNone},
		{"google", "gemini-2.0-flash", ReasoningSupportNone},
		{"google", "gemini-9-ultra", ReasoningSupportNone},
		{"deepseek", "deepseek-chat", ReasoningSupportNone},
		{"deepseek", "deepseek-coder-v4", ReasoningSupportNone},
		{"mistral", "mistral-large-latest", ReasoningSupportNone},
		{"", "claude-sonnet-5", ReasoningSupportOptional}, // provider from the seed catalog
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ReasoningSupportFor(c.provider, c.name), "%s/%s", c.provider, c.name)
	}
}

func TestEffectiveReasoningEffort(t *testing.T) {
	assert.Equal(t, ReasoningEffort(""), EffectiveReasoningEffort(ReasoningSupportOptional, ""))
	assert.Equal(t, ReasoningEffortOff, EffectiveReasoningEffort(ReasoningSupportOptional, ReasoningEffortOff))
	assert.Equal(t, ReasoningEffort(""), EffectiveReasoningEffort(ReasoningSupportNone, ReasoningEffortHigh))
	assert.Equal(t, ReasoningEffortMedium, EffectiveReasoningEffort(ReasoningSupportToggle, ReasoningEffortHigh))
	assert.Equal(t, ReasoningEffortOff, EffectiveReasoningEffort(ReasoningSupportToggle, ReasoningEffortOff))
	assert.Equal(t, ReasoningEffortLow, EffectiveReasoningEffort(ReasoningSupportAlways, ReasoningEffortOff))
	assert.Equal(t, ReasoningEffortHigh, EffectiveReasoningEffort(ReasoningSupportAlways, ReasoningEffortHigh))
}

func TestTurnSettingValues_JSONAndValidation(t *testing.T) {
	var v TurnSettingValues
	require.NoError(t, json.Unmarshal([]byte(`{"reasoning_effort":"medium","agent_loop":""}`), &v))
	require.NoError(t, v.Validate())
	n := v.Normalized()
	assert.Equal(t, ReasoningEffortMedium, *n.ReasoningEffort)
	assert.Nil(t, n.AgentLoop, `"" clears`)

	require.Error(t, TurnSettingValues{ReasoningEffort: effortPtr("max")}.Validate())
	require.Error(t, TurnSettingValues{AgentLoop: loopPtr("react")}.Validate())

	raw, err := json.Marshal(TurnSettingValues{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"reasoning_effort":null,"agent_loop":null}`, string(raw))
}

func TestFirstSet(t *testing.T) {
	assert.Equal(t, ReasoningEffortHigh, *FirstSetReasoningEffort(nil, effortPtr(""), effortPtr(ReasoningEffortHigh), effortPtr(ReasoningEffortLow)))
	assert.Nil(t, FirstSetReasoningEffort(nil, nil))
	assert.Equal(t, AgentLoopAdvanced, *FirstSetAgentLoop(nil, loopPtr(AgentLoopAdvanced)))
}

func TestJobTurnSettingsContext(t *testing.T) {
	ctx := WithJobTurnSettings(context.Background(), TurnSettingValues{AgentLoop: loopPtr(AgentLoopAdvanced)})
	lvl := JobTurnSettingsFromContext(ctx)
	require.NotNil(t, lvl.AgentLoop)
	assert.Equal(t, AgentLoopAdvanced, *lvl.AgentLoop)

	copied := CopyJobTurnSettings(ctx, context.Background())
	assert.Equal(t, lvl, JobTurnSettingsFromContext(copied))
	assert.Equal(t, TurnSettingLevel{}, JobTurnSettingsFromContext(context.Background()))
}

func TestParseUpdates(t *testing.T) {
	v, change, ok := ParseReasoningEffortUpdate(nil)
	assert.True(t, ok)
	assert.False(t, change)
	assert.Nil(t, v)

	empty := ""
	v, change, ok = ParseReasoningEffortUpdate(&empty)
	assert.True(t, ok)
	assert.True(t, change)
	assert.Nil(t, v)

	high := " HIGH "
	v, _, ok = ParseReasoningEffortUpdate(&high)
	assert.True(t, ok)
	assert.Equal(t, ReasoningEffortHigh, *v)

	bad := "warp"
	_, _, ok = ParseAgentLoopUpdate(&bad)
	assert.False(t, ok)
}
