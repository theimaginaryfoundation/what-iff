package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func ptrOf[T any](v T) *T { return &v }

// The turn's settings follow job → mode → thread, and the effort is mapped onto the model.
func TestResolveTurnSettings_PrecedenceAndModelMapping(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{
		model:         "glm-5.3",
		modelProvider: "zai",
		chat: &models.Chat{TurnSettingValues: models.TurnSettingValues{
			ReasoningEffort: ptrOf(models.ReasoningEffortOff),
			AgentLoop:       ptrOf(models.AgentLoopStandard),
		}},
	}
	mode := &models.Mood{TurnSettingValues: models.TurnSettingValues{AgentLoop: ptrOf(models.AgentLoopAdvanced)}}

	a.resolveTurnSettings(context.Background(), chatCtx, mode)
	assert.Equal(t, models.SettingSourceThread, chatCtx.turnSettings.ReasoningEffortSource)
	assert.Equal(t, models.ReasoningEffortLow, chatCtx.reasoningEffort, "GLM cannot turn reasoning off, so off runs as low")
	assert.Equal(t, models.SettingSourceMode, chatCtx.turnSettings.AgentLoopSource)
	assert.True(t, chatCtx.turnSettings.Advanced())

	jobCtx := models.WithJobTurnSettings(context.Background(), models.TurnSettingValues{
		ReasoningEffort: ptrOf(models.ReasoningEffortHigh),
		AgentLoop:       ptrOf(models.AgentLoopStandard),
	})
	a.resolveTurnSettings(jobCtx, chatCtx, mode)
	assert.Equal(t, models.SettingSourceJob, chatCtx.turnSettings.ReasoningEffortSource)
	assert.Equal(t, models.ReasoningEffortHigh, chatCtx.reasoningEffort)
	assert.False(t, chatCtx.turnSettings.Advanced(), "the job's Standard wins over the mode's Advanced")

	a.resolveTurnSettings(context.Background(), nil, nil) // nil-safe
}

// N1: in a sandboxed thread (the Discord relay) the owner's mode cannot switch turns to Advanced
// or a reasoning effort; only the thread's and a job's own values apply.
func TestResolveTurnSettings_SandboxIgnoresMode(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{model: "gpt-5.1", modelProvider: "openai", chat: &models.Chat{ContextScope: models.ContextScopeSandbox}}
	deepResearch := &models.Mood{TurnSettingValues: models.TurnSettingValues{
		ReasoningEffort: ptrOf(models.ReasoningEffortHigh),
		AgentLoop:       ptrOf(models.AgentLoopAdvanced),
	}}

	a.resolveTurnSettings(context.Background(), chatCtx, deepResearch)
	assert.Equal(t, models.TurnSettings{}, chatCtx.turnSettings)
	assert.Empty(t, chatCtx.reasoningEffort)

	chatCtx.chat.AgentLoop = ptrOf(models.AgentLoopAdvanced)
	a.resolveTurnSettings(context.Background(), chatCtx, deepResearch)
	assert.Equal(t, models.SettingSourceThread, chatCtx.turnSettings.AgentLoopSource, "the thread's own value still applies")
	assert.Equal(t, models.SettingSourceUnset, chatCtx.turnSettings.ReasoningEffortSource)
}

func TestResolveTurnSettings_LocalBackendRunsStandard(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	a := &Agent{logger: zap.New(core), localLLM: true}
	chatCtx := &chatContext{chat: &models.Chat{TurnSettingValues: models.TurnSettingValues{AgentLoop: ptrOf(models.AgentLoopAdvanced)}}}

	a.resolveTurnSettings(context.Background(), chatCtx, nil)
	assert.Equal(t, models.AgentLoopStandard, chatCtx.turnSettings.AgentLoop)
	assert.Equal(t, 1, logs.FilterMessageSnippet("LLM_BACKEND=local").Len())
}

func TestResolveTurnSettings_NoSelectionSendsNothing(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{model: "gpt-5.1", modelProvider: "openai", chat: &models.Chat{}}
	a.resolveTurnSettings(context.Background(), chatCtx, nil)
	assert.Equal(t, models.TurnSettings{}, chatCtx.turnSettings)
	assert.Empty(t, chatCtx.reasoningEffort)
}

func TestThreadActiveMood(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	assert.Nil(t, a.threadActiveMood(context.Background(), nil))
	assert.Nil(t, a.threadActiveMood(context.Background(), &chatContext{chat: &models.Chat{}}), "no active mode")

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery(".*").WillReturnError(errors.New("db down"))
	core, logs := observer.New(zap.WarnLevel)
	a = &Agent{logger: zap.New(core), ds: ds}
	moodID := uuid.New()
	got := a.threadActiveMood(context.Background(), &chatContext{chat: &models.Chat{UserID: uuid.New(), ActiveMoodID: &moodID}})
	assert.Nil(t, got, "a mode that cannot be loaded is skipped")
	assert.Equal(t, 1, logs.FilterMessageSnippet("active mode").Len())
}

func TestWithTurnAgentLoop(t *testing.T) {
	advanced := &chatContext{turnSettings: models.TurnSettings{AgentLoop: models.AgentLoopAdvanced}}
	assert.Equal(t, "advanced", metering.AgentLoopFromContext(withTurnAgentLoop(context.Background(), advanced)))
	assert.Empty(t, metering.AgentLoopFromContext(withTurnAgentLoop(context.Background(), &chatContext{})))
	assert.Empty(t, metering.AgentLoopFromContext(withTurnAgentLoop(context.Background(), nil)))
}

func TestSubagentReasoningEffortContext(t *testing.T) {
	ctx := withSubagentReasoningEffort(context.Background(), models.ReasoningEffortMedium)
	assert.Equal(t, models.ReasoningEffortMedium, subagentReasoningEffort(ctx))
	assert.Empty(t, subagentReasoningEffort(withSubagentReasoningEffort(context.Background(), "")))
}

func TestWithTurnSettingsUsage_NilChatContext(t *testing.T) {
	u := withTurnSettingsUsage(nil, metering.Usage{Model: "m"})
	require.Equal(t, "m", u.Model)
	require.Empty(t, u.AgentLoop)
}
