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

	got := ResolveTurnSettings(TurnSettingLevels{Job: job, Mode: mode, Thread: thread})
	assert.Equal(t, ReasoningEffortHigh, got.ReasoningEffort, "mode overrides thread")
	assert.Equal(t, SettingSourceMode, got.ReasoningEffortSource)
	assert.Equal(t, AgentLoopAdvanced, got.AgentLoop, "job overrides everything")
	assert.Equal(t, SettingSourceJob, got.AgentLoopSource)
	assert.True(t, got.Advanced())

	got = ResolveTurnSettings(TurnSettingLevels{Thread: thread})
	assert.Equal(t, ReasoningEffortLow, got.ReasoningEffort)
	assert.Equal(t, SettingSourceThread, got.ReasoningEffortSource)

	assert.Equal(t, TurnSettings{}, ResolveTurnSettings(TurnSettingLevels{}), "nothing set anywhere is today's behaviour")

	bad := ReasoningEffort("extreme")
	got = ResolveTurnSettings(TurnSettingLevels{Thread: thread, Mode: TurnSettingLevel{ReasoningEffort: &bad}})
	assert.Equal(t, ReasoningEffortLow, got.ReasoningEffort, "invalid stored values are skipped")
}

// A thread without a value of its own inherits, per setting, its personality's and then the
// account default, at each turn.
func TestResolveTurnSettings_Inherits(t *testing.T) {
	personality := TurnSettingLevel{ReasoningEffort: effortPtr(ReasoningEffortHigh)}
	account := TurnSettingLevel{ReasoningEffort: effortPtr(ReasoningEffortLow), AgentLoop: loopPtr(AgentLoopAdvanced)}

	got := ResolveTurnSettings(TurnSettingLevels{Personality: personality, Account: account})
	assert.Equal(t, ReasoningEffortHigh, got.ReasoningEffort)
	assert.Equal(t, SettingSourcePersonality, got.ReasoningEffortSource)
	assert.Equal(t, AgentLoopAdvanced, got.AgentLoop)
	assert.Equal(t, SettingSourceAccount, got.AgentLoopSource)

	got = ResolveTurnSettings(TurnSettingLevels{Thread: TurnSettingLevel{AgentLoop: loopPtr(AgentLoopStandard)}, Account: account})
	assert.Equal(t, AgentLoopStandard, got.AgentLoop, "the thread's own choice wins over the default")
}

// In a sandbox only the job's and thread's own values apply: a mode, the personality or the
// account default cannot start Advanced for the people talking in it.
func TestResolveTurnSettings_SandboxIgnoresOwnerLevels(t *testing.T) {
	advanced := TurnSettingLevel{ReasoningEffort: effortPtr(ReasoningEffortHigh), AgentLoop: loopPtr(AgentLoopAdvanced)}
	got := ResolveTurnSettings(TurnSettingLevels{Mode: advanced, Personality: advanced, Account: advanced, Sandboxed: true})
	assert.Equal(t, TurnSettings{}, got)

	got = ResolveTurnSettings(TurnSettingLevels{Thread: advanced, Mode: TurnSettingLevel{AgentLoop: loopPtr(AgentLoopStandard)}, Sandboxed: true})
	assert.Equal(t, AgentLoopAdvanced, got.AgentLoop, "an explicit thread value still applies")
	assert.Equal(t, SettingSourceThread, got.AgentLoopSource)
}

func TestReasoningSupportFor(t *testing.T) {
	cases := []struct {
		provider, name string
		want           ReasoningSupport
	}{
		{"openai", "gpt-5.1", ReasoningSupportOptional},
		{"openai", "gpt-5-mini", ReasoningSupportAlways}, // minimal would rule out web_search
		{"openai", "gpt-5", ReasoningSupportAlways},
		{"openai", "gpt-5-codex", ReasoningSupportAlways},
		{"openai", "gpt-5.1-codex-mini", ReasoningSupportAlways},
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
		{"zai", "glm-5.3", ReasoningSupportAlways},
		{"zai", "glm-4.6", ReasoningSupportAlways},
		{"zai", "glm-4.5v", ReasoningSupportAlways},
		{"zai", "glm-3-turbo", ReasoningSupportNone},
		{"xiaomi", "mimo-v2.5-pro", ReasoningSupportToggle},
		{"xiaomi", "mimo-audio", ReasoningSupportNone},
		{"qwen", "qwen3-235b-a22b-thinking-2507", ReasoningSupportAlways},
		{"qwen", "qwen3-235b-a22b-instruct-2507", ReasoningSupportNone},
		{"qwen", "qwen3-coder-plus", ReasoningSupportNone},
		{"openai", "o3-deep-research", ReasoningSupportNone},
		{"anthropic", "claude-haiku-5-5", ReasoningSupportAlways},
		{"mistral", "mistral-large-latest", ReasoningSupportNone},
		{"", "claude-sonnet-5", ReasoningSupportOptional}, // provider from the seed catalog
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ReasoningSupportFor(c.provider, c.name), "%s/%s", c.provider, c.name)
	}
}

// Each family's native level, from its provider's docs (reasoning_models.go).
func TestNativeReasoningEffort(t *testing.T) {
	cases := []struct {
		provider, name string
		want           ReasoningEffort
	}{
		{"anthropic", "claude-opus-5-5", ReasoningEffortMedium},
		{"anthropic", "claude-haiku-5-5", ReasoningEffortMedium},
		{"anthropic", "claude-sonnet-5-5", ReasoningEffortHigh},
		{"anthropic", "claude-opus-5", ReasoningEffortHigh},
		{"anthropic", "claude-fable-5-1", ReasoningEffortHigh},
		{"anthropic", "claude-opus-4-8", ReasoningEffortOff},
		{"anthropic", "claude-sonnet-4-5", ReasoningEffortOff},
		{"openai", "gpt-5", ReasoningEffortMedium},
		{"openai", "gpt-5-mini", ReasoningEffortMedium},
		{"openai", "gpt-5.1", ReasoningEffortOff},
		{"openai", "gpt-5.4-mini", ReasoningEffortOff},
		{"openai", "gpt-5.1-codex", ReasoningEffortMedium},
		{"openai", "gpt-5.5", ReasoningEffortMedium},
		{"openai", "o3", ReasoningEffortMedium},
		{"openai", "gpt-4o", ReasoningEffortOff},
		{"openai", "gpt-5-chat-latest", ReasoningEffortOff},
		{"openai", "o3-pro", ReasoningEffortOff},
		{"openai", "o4-mini", ReasoningEffortMedium},
		{"openai", "gpt-5.2", ReasoningEffortOff},
		{"openai", "gpt-5.6", ReasoningEffortMedium},
		{"google", "gemini-2.5-flash", ReasoningEffortHigh},
		{"google", "gemini-3.1-pro-preview", ReasoningEffortHigh},
		{"google", "gemini-3.8-flash", ReasoningEffortMedium},
		{"google", "gemini-3.6-flash", ReasoningEffortMedium},
		{"google", "gemini-3-pro-preview", ReasoningEffortHigh},
		{"google", "gemini-3-flash-preview", ReasoningEffortHigh},
		{"google", "gemini-2.5-pro", ReasoningEffortHigh},
		{"google", "gemini-3-pro-image-preview", ReasoningEffortOff},
		{"qwen", "qwen3-235b-a22b-thinking-2507", ReasoningEffortHigh},
		{"qwen", "qwen3.7-plus", ReasoningEffortHigh},
		{"qwen", "qwen-plus", ReasoningEffortOff},
		{"qwen", "qwen3-32b", ReasoningEffortOff},
		{"qwen", "qwen3.5-plus", ReasoningEffortHigh},
		{"qwen", "qwen3.6-flash", ReasoningEffortHigh},
		{"qwen", "qwen3.8-max", ReasoningEffortHigh},
		{"qwen", "qwen3.8-2.4t-a95b", ReasoningEffortHigh},
		{"qwen", "qwen3.7-max-preview", ReasoningEffortHigh},
		{"qwen", "qwen3.6-coder-plus", ReasoningEffortOff},
		{"qwen", "qwen3-235b-a22b-instruct-2507", ReasoningEffortOff},
		{"qwen", "qwen-flash", ReasoningEffortOff},
		{"deepseek", "deepseek-v4-pro", ReasoningEffortHigh},
		{"mistral", "mistral-large-4", ReasoningEffortOff},
		{"zai", "glm-5", ReasoningEffortHigh},
		{"xiaomi", "mimo-v2-pro", ReasoningEffortMedium},
		{"openai", "unknown-model", ReasoningEffortOff},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, NativeReasoningEffort(c.provider, c.name), "%s/%s", c.provider, c.name)
	}
}

// ReasoningEfforts lists exactly the valid levels, lowest first: ReasoningEffortAboveNative ranks by
// it, so a level added to Valid but not here (or out of order) would misrank.
func TestReasoningEffortsRankEveryValidLevel(t *testing.T) {
	assert.Equal(t, []ReasoningEffort{ReasoningEffortOff, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}, ReasoningEfforts)
	for _, e := range ReasoningEfforts {
		assert.True(t, e.Valid(), e)
		_, ok := effortRank(e)
		assert.True(t, ok, e)
	}
	_, ok := effortRank("xhigh")
	assert.False(t, ok)
	assert.Empty(t, ReasoningEffortAboveNative("openai", "gpt-5.1", "xhigh"), "an unknown level is never reported")
}

// Only an effort above the native level is reported: a lower or equal one costs the model no more.
func TestReasoningEffortAboveNative(t *testing.T) {
	assert.Empty(t, ReasoningEffortAboveNative("anthropic", "claude-opus-5-5", ReasoningEffortLow))
	assert.Empty(t, ReasoningEffortAboveNative("anthropic", "claude-opus-5-5", ReasoningEffortMedium))
	assert.Equal(t, ReasoningEffortHigh, ReasoningEffortAboveNative("anthropic", "claude-opus-5-5", ReasoningEffortHigh))
	assert.Empty(t, ReasoningEffortAboveNative("zai", "glm-5", ReasoningEffortHigh), "GLM reasons at its deepest unasked")
	assert.Empty(t, ReasoningEffortAboveNative("xiaomi", "mimo-v2-pro", ReasoningEffortMedium), "on is MiMo's default")
	assert.Equal(t, ReasoningEffortLow, ReasoningEffortAboveNative("openai", "gpt-5.1", ReasoningEffortLow))
	assert.Empty(t, ReasoningEffortAboveNative("openai", "gpt-5.1", ReasoningEffortOff))
	assert.Empty(t, ReasoningEffortAboveNative("openai", "gpt-5.1", ""))
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

func TestTurnSettingKeysAndKeepOmitted(t *testing.T) {
	effort, loop := TurnSettingKeys([]byte(`{"name":"x","reasoning_effort":""}`))
	assert.True(t, effort)
	assert.False(t, loop)
	effort, loop = TurnSettingKeys([]byte(`not json`))
	assert.False(t, effort || loop)

	stored := TurnSettingValues{ReasoningEffort: effortPtr(ReasoningEffortHigh), AgentLoop: loopPtr(AgentLoopAdvanced)}
	got := KeepOmittedTurnSettings(TurnSettingValues{}, stored, true, false)
	assert.Nil(t, got.ReasoningEffort, "sent (as a clear) wins")
	assert.Equal(t, AgentLoopAdvanced, *got.AgentLoop, "omitted is kept")
}

func TestReasoningSupportWithOverride(t *testing.T) {
	always := ReasoningSupportAlways
	assert.Equal(t, ReasoningSupportAlways, ReasoningSupportWithOverride(&always, "openai", "gpt-4o"), "an admin override wins")
	assert.Equal(t, ReasoningSupportNone, ReasoningSupportWithOverride(nil, "openai", "gpt-4o"))
	bad := ReasoningSupport("maybe")
	assert.Equal(t, ReasoningSupportOptional, ReasoningSupportWithOverride(&bad, "openai", "gpt-5.1"), "an invalid override is ignored")
}
