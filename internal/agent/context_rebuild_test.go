package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func testAgentWithHistoryOverride(history []*models.ChatMessage) *Agent {
	return &Agent{
		logger:    zap.NewNop(),
		telemetry: &telemetry.Telemetry{Logger: zap.NewNop()},
		testHooks: agentTestHooks{
			LoadHistoryOverride: func(_ context.Context, _, _, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
				return history
			},
		},
	}
}

func makeChatContext(model string) *chatContext {
	return &chatContext{
		model: model,
		chat: &models.Chat{
			ID:                uuid.New(),
			UserID:            uuid.New(),
			SystemPrompt:      "agent prompt",
			Scratchpad:        "scratch",
			CheckpointSummary: "summary",
			ToolsEnabled:      false,
		},
		memories: []string{"m1"},
	}
}

func makeHistory() []*models.ChatMessage {
	return []*models.ChatMessage{
		{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "u1"},
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "a1"},
		{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "u2"},
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "a2"},
	}
}

func itemMessageText(item responses.ResponseInputItemUnionParam) string {
	b, _ := json.Marshal(item)
	return string(b)
}

func claudeMessageText(msg anthropic.MessageParam) string {
	if len(msg.Content) == 0 || msg.Content[0].OfText == nil {
		return ""
	}
	return msg.Content[0].OfText.Text
}

func TestBuildInputMessages_ModelSwitchRetainsHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	chatCtx := makeChatContext("gpt-5.1")
	history := makeHistory()

	a := testAgentWithHistoryOverride(history)

	current := &models.ChatMessage{
		ID:      uuid.New(),
		ChatID:  chatCtx.chat.ID,
		Message: "current-user",
		Origin:  models.MessageOriginUser,
	}

	modelCtx, err := a.buildModelContextForChatMessage(ctx, userID, current, chatCtx, nil)
	require.NoError(t, err)
	msgs := provider.RenderOpenAIInputItems(modelCtx)
	require.GreaterOrEqual(t, len(msgs), 8)

	var all []string
	for _, m := range msgs {
		all = append(all, itemMessageText(m))
	}
	joined := strings.Join(all, "\n")
	require.Contains(t, joined, "scratch")
	require.Contains(t, joined, "summary")
	require.Contains(t, joined, "u1")
	require.Contains(t, joined, "a1")
	require.Contains(t, joined, "u2")
	require.Contains(t, joined, "a2")
	require.Contains(t, joined, "current-user")
}

func TestBuildClaudeParams_ModelSwitchRetainsHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	chatCtx := makeChatContext("claude-sonnet-4-6")
	history := makeHistory()

	a := testAgentWithHistoryOverride(history)

	current := &models.ChatMessage{
		ID:      uuid.New(),
		ChatID:  chatCtx.chat.ID,
		Message: "current-user",
		Origin:  models.MessageOriginUser,
	}

	modelCtx, err := a.buildModelContextForChatMessage(ctx, userID, current, chatCtx, nil)
	require.NoError(t, err)
	params := modelCtx.BuildClaudeParams(chatCtx.model)
	require.NotEmpty(t, params.Messages)

	var all []string
	for _, m := range params.Messages {
		all = append(all, claudeMessageText(m))
	}
	joined := strings.Join(all, "\n")
	require.Contains(t, joined, "u1")
	require.Contains(t, joined, "a1")
	require.Contains(t, joined, "u2")
	require.Contains(t, joined, "a2")
	require.Contains(t, joined, "current-user")
	require.Contains(t, joined, "m1")
}

func TestBuildInputMessages_AsyncJobShape_NoPersistedUserHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	chatCtx := makeChatContext("gpt-5.1")
	assistantOnly := []*models.ChatMessage{
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "a1"},
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "a2"},
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "a3"},
	}

	a := testAgentWithHistoryOverride(assistantOnly)

	current := &models.ChatMessage{
		ID:      uuid.New(),
		ChatID:  chatCtx.chat.ID,
		Message: "job-prompt-now",
		Origin:  models.MessageOriginUser,
	}

	modelCtx, err := a.buildModelContextForChatMessage(ctx, userID, current, chatCtx, nil)
	require.NoError(t, err)
	msgs := provider.RenderOpenAIInputItems(modelCtx)

	var all []string
	for _, m := range msgs {
		all = append(all, itemMessageText(m))
	}
	joined := strings.Join(all, "\n")
	require.Contains(t, joined, "a1")
	require.Contains(t, joined, "a2")
	require.Contains(t, joined, "a3")
	require.NotContains(t, joined, "old-job-user")
	require.Contains(t, joined, "job-prompt-now")
}

func TestBuildInputMessages_PersistedAdditionalContextFromHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	chatCtx := makeChatContext("gpt-5.1")
	history := []*models.ChatMessage{
		{
			ID:      uuid.New(),
			Origin:  models.MessageOriginUser,
			Message: "prior",
			AdditionalContext: []models.AdditionalContextItem{
				{Type: models.AdditionalContextTypeMemory, Content: "persisted-from-prior-turn"},
			},
		},
	}
	a := testAgentWithHistoryOverride(history)
	current := &models.ChatMessage{
		ID:      uuid.New(),
		ChatID:  chatCtx.chat.ID,
		Message: "next",
		Origin:  models.MessageOriginUser,
	}
	modelCtx, err := a.buildModelContextForChatMessage(ctx, userID, current, chatCtx, nil)
	require.NoError(t, err)
	msgs := provider.RenderOpenAIInputItems(modelCtx)
	var all []string
	for _, m := range msgs {
		all = append(all, itemMessageText(m))
	}
	joined := strings.Join(all, "\n")
	require.Contains(t, joined, "persisted-from-prior-turn")
	require.Contains(t, joined, "m1")
}
