package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func TestLastPriorAssistantExpressionSnapshot_historyNewestWins(t *testing.T) {
	t.Parallel()
	k1, k2 := "first", "second"
	l1, l2 := "label a", "label b"
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, Message: "old", GenerationExpressionKey: &k1, GenerationExpressionLabel: &l1},
		{Origin: models.MessageOriginUser, Message: "mid"},
		{Origin: models.MessageOriginAssistant, Message: "new", GenerationExpressionKey: &k2, GenerationExpressionLabel: &l2},
	}
	key, labelText, reasoningText, ok := lastPriorAssistantExpressionSnapshot(history, nil)
	require.True(t, ok)
	require.Equal(t, "second", key)
	require.Equal(t, "label b", labelText)
	require.Empty(t, reasoningText)
}

func TestLastPriorAssistantExpressionSnapshot_carryOverFallback(t *testing.T) {
	t.Parallel()
	k := "carry"
	l := "from checkpoint window"
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginUser, Message: "u"},
	}
	carry := [][2]*models.ChatMessage{
		{
			{Origin: models.MessageOriginUser, Message: "u0"},
			{Origin: models.MessageOriginAssistant, Message: "a0", GenerationExpressionKey: &k, GenerationExpressionLabel: &l},
		},
	}
	key, labelText, reasoningText, ok := lastPriorAssistantExpressionSnapshot(history, carry)
	require.True(t, ok)
	require.Equal(t, "carry", key)
	require.Equal(t, "from checkpoint window", labelText)
	require.Empty(t, reasoningText)
}

func TestAppendPriorTurnExpressionContinuity_withPortrait(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	imageID := uuid.New()
	k := "happy"
	history := []*models.ChatMessage{
		{
			Origin:                      models.MessageOriginAssistant,
			Message:                     "prior reply",
			GenerationExpressionKey:     &k,
			GenerationExpressionImageID: &imageID,
		},
	}
	m := &provider.ModelContext{}
	m.Append(provider.SegmentKindHistoryTurn, provider.RoleAssistant, "prior reply", true)

	cache := newExpressionPortraitThumbCache(10)
	cache.put(userID.String()+"/"+imageID.String(), []byte{0x89, 0x50}, "image/png")

	b := testMessageContextBuilder(t)
	b.expressionThumbCache = cache
	b.appendPriorTurnExpressionContinuity(context.Background(), userID, m, nil, history)

	require.Len(t, m.Segments, 3)
	require.Equal(t, provider.SegmentKindHistoryTurn, m.Segments[0].Kind)
	require.Empty(t, m.Segments[0].UserImages)
	require.Equal(t, provider.SegmentKindExpressionPortrait, m.Segments[1].Kind)
	require.Len(t, m.Segments[1].UserImages, 1)
	require.Equal(t, provider.SegmentKindDeveloperContext, m.Segments[2].Kind)
	require.Contains(t, m.Segments[2].Content, `"happy"`)
	require.Contains(t, m.Segments[2].Content, "preceding user message")
}

func TestAppendPriorTurnExpressionContinuity_withLabel(t *testing.T) {
	t.Parallel()
	k := "mischievous"
	l := "playful teasing"
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, GenerationExpressionKey: &k, GenerationExpressionLabel: &l},
	}
	m := &provider.ModelContext{}
	b := testMessageContextBuilder(t)
	b.appendPriorTurnExpressionContinuity(context.Background(), uuid.Nil, m, nil, history)
	require.Len(t, m.Segments, 1)
	require.Equal(t, provider.SegmentKindDeveloperContext, m.Segments[0].Kind)
	require.Equal(t, `The previous *assistant* message was classified with the expression: "mischievous" (usage hint: playful teasing).`, m.Segments[0].Content)
}

func TestAppendPriorTurnExpressionContinuity_withReasoning(t *testing.T) {
	t.Parallel()
	k := "happy"
	l := "warm"
	r := "The reply was welcoming after the user shared good news."
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, GenerationExpressionKey: &k, GenerationExpressionLabel: &l, GenerationExpressionReasoning: &r},
	}
	m := &provider.ModelContext{}
	b := testMessageContextBuilder(t)
	b.appendPriorTurnExpressionContinuity(context.Background(), uuid.Nil, m, nil, history)
	require.Len(t, m.Segments, 1)
	require.Equal(t, provider.SegmentKindDeveloperContext, m.Segments[0].Kind)
	require.Equal(t, `The previous *assistant* message was classified with the expression: "happy" (usage hint: warm). Rationale: The reply was welcoming after the user shared good news.`, m.Segments[0].Content)
}

func TestAppendPriorTurnExpressionContinuity_keyOnly(t *testing.T) {
	t.Parallel()
	k := "neutral"
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, GenerationExpressionKey: &k},
	}
	m := &provider.ModelContext{}
	b := testMessageContextBuilder(t)
	b.appendPriorTurnExpressionContinuity(context.Background(), uuid.Nil, m, nil, history)
	require.Len(t, m.Segments, 1)
	require.Equal(t, provider.SegmentKindDeveloperContext, m.Segments[0].Kind)
	require.Equal(t, `The previous *assistant* message was classified with the expression: "neutral".`, m.Segments[0].Content)
}

func TestMessageContextBuilder_Build_skipsExpressionContinuityWhenDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	userID := uuid.New()
	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "be helpful"}
	k := "happy"
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, Message: "prior reply", GenerationExpressionKey: &k},
	}
	b := testMessageContextBuilder(t)
	b.loadHistoryOverride = func(_ context.Context, _, _ uuid.UUID, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
		return history
	}
	mc, err := b.build(ctx, messageContextBuildRequest{
		UserID:                   userID,
		Chat:                     chat,
		UserPrompt:               "new user text",
		ExpressionsEnabled:       false,
		IncludeAttachmentContext: true,
	})
	require.NoError(t, err)
	for _, seg := range mc.Segments {
		require.NotContains(t, seg.Content, "classified with the expression")
	}
}

func TestAppendPriorTurnExpressionContinuity_nilModelCtx(t *testing.T) {
	t.Parallel()
	k := "neutral"
	history := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, GenerationExpressionKey: &k},
	}
	b := testMessageContextBuilder(t)
	require.NotPanics(t, func() {
		b.appendPriorTurnExpressionContinuity(context.Background(), uuid.Nil, nil, nil, history)
	})
}

func testMessageContextBuilder(t *testing.T) *messageContextBuilder {
	t.Helper()
	b, err := newMessageContextBuilder(nil, testTelemetry(t), nil, nil, nil)
	require.NoError(t, err)
	return b
}

func testTelemetry(t *testing.T) *telemetry.Telemetry {
	t.Helper()
	return &telemetry.Telemetry{Logger: zap.NewNop()}
}
