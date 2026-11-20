package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// turnOptionsTestBuilder is a context builder whose history is two prior turns.
func turnOptionsTestBuilder(t *testing.T, chatID uuid.UUID) *messageContextBuilder {
	t.Helper()
	sent := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	history := []*models.ChatMessage{
		{ID: uuid.New(), ChatID: chatID, Origin: models.MessageOriginUser, Message: "HIST-USER", SentAt: sent},
		{ID: uuid.New(), ChatID: chatID, Origin: models.MessageOriginAssistant, Message: "HIST-ASSISTANT", SentAt: sent.Add(time.Minute)},
	}
	b, err := newMessageContextBuilder(nil, &telemetry.Telemetry{Logger: zap.NewNop()}, nil,
		func(_ context.Context, _, _, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
			return history
		}, nil)
	require.NoError(t, err)
	return b
}

func turnOptionsTestRequest(chat *models.Chat) messageContextBuildRequest {
	return messageContextBuildRequest{
		UserID:                     uuid.New(),
		Chat:                       chat,
		UserPrompt:                 "[sys:Fri 2026-11-20 09:05:00 +00:00] NEW-TURN",
		CurrentMessage:             &models.ChatMessage{ID: uuid.New(), ChatID: chat.ID, Origin: models.MessageOriginUser, Message: "NEW-TURN"},
		Memories:                   []string{"MEMORY-ONE"},
		AdditionalDeveloperContext: "TOOL-NOTES",
	}
}

func turnOptionsTestChat() *models.Chat {
	return &models.Chat{
		ID:                uuid.New(),
		SystemPrompt:      "PERSONALITY",
		CheckpointSummary: "SUMMARY",
		Scratchpad:        "SCRATCH",
	}
}

func segmentKinds(mc *provider.ModelContext) []provider.ModelContextSegmentKind {
	out := make([]provider.ModelContextSegmentKind, 0, len(mc.Segments))
	for _, s := range mc.Segments {
		out = append(out, s.Kind)
	}
	return out
}

// The design's order: system block (base + personality, summary, scratchpad), pre_context,
// history, WhatIff's per-turn tail (memories, tool notes), post_context, the new user message.
func TestBuild_TurnContextPlacement(t *testing.T) {
	t.Parallel()
	chat := turnOptionsTestChat()
	b := turnOptionsTestBuilder(t, chat.ID)
	req := turnOptionsTestRequest(chat)
	req.PreContext = "PRE-LORE"
	req.PostContext = "POST-SCENE"

	mc, err := b.build(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, []provider.ModelContextSegmentKind{
		provider.SegmentKindSystemPrompt,
		provider.SegmentKindCheckpointSummary,
		provider.SegmentKindScratchpad,
		provider.SegmentKindPreContext,
		provider.SegmentKindHistoryTurn,
		provider.SegmentKindHistoryTurn,
		provider.SegmentKindMemoryContext,
		provider.SegmentKindDeveloperContext,
		provider.SegmentKindPostContext,
		provider.SegmentKindUserMessage,
	}, segmentKinds(mc))

	pre := mc.Segments[3]
	assert.Equal(t, "PRE-LORE", pre.Content)
	assert.True(t, pre.Cacheable, "pre_context belongs to the cached prefix")
	post := mc.Segments[len(mc.Segments)-2]
	assert.Equal(t, "POST-SCENE", post.Content)
	assert.False(t, post.Cacheable)
	assert.Equal(t, provider.RoleDeveloper, post.Role)
	assert.Contains(t, mc.Segments[len(mc.Segments)-1].Content, "[sys:", "the new user message keeps its time stamp")
}

// A turn without options is built exactly as before: no turn segments, and the same request bytes
// on every provider family as a turn with options once its two turn segments are removed.
func TestBuild_OrdinaryTurnUnchanged(t *testing.T) {
	t.Parallel()
	chat := turnOptionsTestChat()
	b := turnOptionsTestBuilder(t, chat.ID)
	req := turnOptionsTestRequest(chat)

	ordinary, err := b.build(context.Background(), req)
	require.NoError(t, err)
	assert.NotContains(t, segmentKinds(ordinary), provider.SegmentKindPreContext)
	assert.NotContains(t, segmentKinds(ordinary), provider.SegmentKindPostContext)
	assert.Same(t, ordinary, ordinary.WithoutTurnContext())

	req.PreContext = "PRE-LORE"
	req.PostContext = "POST-SCENE"
	withOptions, err := b.build(context.Background(), req)
	require.NoError(t, err)
	stripped := withOptions.WithoutTurnContext()

	render := map[string]func(*provider.ModelContext) any{
		"claude": func(mc *provider.ModelContext) any {
			return mc.BuildClaudeParams("claude-test")
		},
		"openai": func(mc *provider.ModelContext) any {
			return mc.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{Model: "gpt-test"})
		},
		"chat_completions": func(mc *provider.ModelContext) any {
			return mc.BuildOpenAIChatCompletionParams("chat-test")
		},
	}
	for name, fn := range render {
		want, err := json.Marshal(fn(ordinary))
		require.NoError(t, err)
		got, err := json.Marshal(fn(stripped))
		require.NoError(t, err)
		assert.JSONEq(t, string(want), string(got), "%s: options must add their two segments and change nothing else", name)
		assert.NotContains(t, string(want), "PRE-LORE")
		assert.NotContains(t, string(want), "POST-SCENE")
	}
}

// Whitespace-only context adds nothing, like empty.
func TestBuild_BlankTurnContextAddsNothing(t *testing.T) {
	t.Parallel()
	chat := turnOptionsTestChat()
	b := turnOptionsTestBuilder(t, chat.ID)
	req := turnOptionsTestRequest(chat)
	req.PreContext = "  \n"
	req.PostContext = "\t"
	mc, err := b.build(context.Background(), req)
	require.NoError(t, err)
	assert.NotContains(t, segmentKinds(mc), provider.SegmentKindPreContext)
	assert.NotContains(t, segmentKinds(mc), provider.SegmentKindPostContext)
}

func TestApplyTurnOutputCap(t *testing.T) {
	t.Parallel()
	mc := &provider.ModelContext{}
	mc.AppendUserMessage(provider.RoleUser, "hi", nil, false)

	ordinary := &chatContext{}
	capped := &chatContext{turnOptions: &models.TurnOptions{MaxOutputTokens: 800}}

	t.Run("responses", func(t *testing.T) {
		t.Parallel()
		params := mc.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{Model: "gpt-test", MaxOutputTokens: provider.DefaultMaxContentLength})
		before, _ := json.Marshal(params)
		applyTurnOutputCapResponses(&params, ordinary)
		after, _ := json.Marshal(params)
		assert.Equal(t, string(before), string(after), "no options: request unchanged")

		applyTurnOutputCapResponses(&params, capped)
		assert.Equal(t, int64(800), params.MaxOutputTokens.Value)
	})
	t.Run("claude after a reasoning effort raised the cap", func(t *testing.T) {
		t.Parallel()
		params := mc.BuildClaudeParams("claude-sonnet-4-6")
		provider.ApplyClaudeReasoning(&params, models.ReasoningEffortHigh)
		provider.RaiseClaudeStreamingOutputCap(&params, models.ReasoningEffortHigh)
		before, _ := json.Marshal(params)
		applyTurnOutputCapClaude(&params, ordinary)
		after, _ := json.Marshal(params)
		assert.Equal(t, string(before), string(after), "no options: request unchanged")

		applyTurnOutputCapClaude(&params, capped)
		assert.Equal(t, int64(800), params.MaxTokens, "passed as-is")
	})
	t.Run("chat completions", func(t *testing.T) {
		t.Parallel()
		params := mc.BuildOpenAIChatCompletionParams("chat-test")
		before, _ := json.Marshal(params)
		applyTurnOutputCapChat(&params, ordinary)
		applyTurnOutputCapChat(&params, nil)
		after, _ := json.Marshal(params)
		assert.Equal(t, string(before), string(after), "no options: request unchanged")

		applyTurnOutputCapChat(&params, capped)
		assert.Equal(t, int64(800), params.MaxCompletionTokens.Value)
	})
	t.Run("nil params are ignored", func(t *testing.T) {
		t.Parallel()
		applyTurnOutputCapResponses((*responses.ResponseNewParams)(nil), capped)
		applyTurnOutputCapClaude((*anthropic.MessageNewParams)(nil), capped)
		applyTurnOutputCapChat((*openai.ChatCompletionNewParams)(nil), capped)
	})
}

// Turn options are request-only: they never reach the message's JSON (the API, the archive) and
// the datastore has no column for them, so a turn's pre/post context is gone with the turn.
func TestTurnOptions_NeverSerialized(t *testing.T) {
	t.Parallel()
	modelID := uuid.New()
	msg := models.ChatMessage{
		ID:      uuid.New(),
		Message: "NEW-TURN",
		TurnOptions: &models.TurnOptions{
			PreContext:      "PRE-LORE",
			PostContext:     "POST-SCENE",
			ModelID:         &modelID,
			MaxOutputTokens: 800,
		},
	}
	out, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "PRE-LORE")
	assert.NotContains(t, string(out), "POST-SCENE")
	assert.NotContains(t, string(out), modelID.String())
	assert.Contains(t, string(out), "NEW-TURN")
}

func TestTurnOptions_NilAccessors(t *testing.T) {
	t.Parallel()
	var o *models.TurnOptions
	assert.Empty(t, o.TurnPreContext())
	assert.Empty(t, o.TurnPostContext())
	assert.Nil(t, o.TurnModelID())
	assert.Zero(t, o.TurnMaxOutputTokens())
	assert.Zero(t, (&models.TurnOptions{MaxOutputTokens: -5}).TurnMaxOutputTokens())
	assert.Zero(t, (*chatContext)(nil).turnMaxOutputTokens())
}

func TestRunChatOnModel_ChangesOnlyTheRunCopy(t *testing.T) {
	t.Parallel()
	chat := &models.Chat{ID: uuid.New(), ModelID: uuid.New(), ModelName: "chat-model", ToolsEnabled: false}
	runCopy := *chat
	model := &models.Model{ID: uuid.New(), Name: "turn-model", ToolSupport: true}
	runChatOnModel(&runCopy, model)
	assert.Equal(t, model.ID, runCopy.ModelID)
	assert.Equal(t, "turn-model", runCopy.ModelName)
	assert.True(t, runCopy.ToolsEnabled)
	assert.Equal(t, "chat-model", chat.ModelName)
}
