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

func TestMessageContextBuilder_Build_InjectsEntityCardsAfterMemories(t *testing.T) {
	t.Parallel()
	tel := &telemetry.Telemetry{Logger: zap.NewNop()}
	b, err := newMessageContextBuilder(nil, tel, nil, func(_ context.Context, _, _ uuid.UUID, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
		return nil
	}, nil)
	require.NoError(t, err)

	cards := "People and things mentioned in this message:\n- John Park (person): manager"
	mc, err := b.build(context.Background(), messageContextBuildRequest{
		UserID:      uuid.New(),
		Chat:        &models.Chat{ID: uuid.New(), SystemPrompt: "be helpful"},
		UserPrompt:  "ask John",
		Memories:    []string{"mem1"},
		EntityCards: cards,
	})
	require.NoError(t, err)

	memoryAt, cardsAt := -1, -1
	for i, s := range mc.Segments {
		switch {
		case s.Kind == provider.SegmentKindMemoryContext:
			memoryAt = i
		case s.Kind == provider.SegmentKindDeveloperContext && s.Content == cards:
			cardsAt = i
		}
	}
	require.NotEqual(t, -1, cardsAt, "cards are injected as developer context, which every provider renders")
	require.Greater(t, cardsAt, memoryAt)
	require.Equal(t, provider.SegmentKindUserMessage, mc.Segments[len(mc.Segments)-1].Kind)

	empty, err := b.build(context.Background(), messageContextBuildRequest{
		UserID: uuid.New(), Chat: &models.Chat{ID: uuid.New()}, UserPrompt: "hi", EntityCards: "  ",
	})
	require.NoError(t, err)
	for _, s := range empty.Segments {
		require.NotEqual(t, provider.SegmentKindDeveloperContext, s.Kind, "no cards, no segment")
	}
}

func TestSpotEntityCards_NilToolIsANoop(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	require.Empty(t, a.spotEntityCards(context.Background(), uuid.New(), uuid.New(), "hello John"))
}
