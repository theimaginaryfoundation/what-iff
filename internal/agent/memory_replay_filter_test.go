package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// In an ordinary chat, a memory archived since an earlier turn loaded it is not replayed from that
// turn (ADR 0x036); active memories, id-less lines and other context still are.
func TestBuild_OrdinaryChatDropsReplayedMemoriesNoLongerActive(t *testing.T) {
	t.Parallel()
	activeID, archivedID := uuid.New(), uuid.New()
	history := []*models.ChatMessage{{
		ID: uuid.New(), Origin: models.MessageOriginUser, Message: "earlier question",
		AdditionalContext: []models.AdditionalContextItem{
			memoryItem(&activeID, "Prefers dark mode"),
			memoryItem(&archivedID, "Lives in Berlin"),
			memoryItem(nil, "A legacy memory line without an id"),
			{Type: models.AdditionalContextTypeUserName, Content: "The user's name is Ada"},
		},
	}}
	b := testBuilderWithLookup(t, history, nil)
	var asked []uuid.UUID
	b.activeMemoryLookup = func(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
		asked = ids
		return map[uuid.UUID]struct{}{activeID: {}}, nil
	}

	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "p"}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)

	mem := segmentText(mc, provider.SegmentKindMemoryContext)
	require.Contains(t, mem, "Prefers dark mode")
	require.NotContains(t, mem, "Lives in Berlin", "an archived memory is not replayed")
	require.Contains(t, mem, "legacy memory line", "a line without an id is kept, as before")
	require.Contains(t, mem, "Ada")
	require.ElementsMatch(t, []uuid.UUID{activeID, archivedID}, asked, "one lookup covers every persisted id")
	for _, ref := range mc.MemoryRefs {
		require.NotEqual(t, archivedID.String(), ref.MemoryID)
	}
}

// A failed re-check in an ordinary chat keeps the replay as it was rather than dropping context.
func TestBuild_OrdinaryChatKeepsReplayWhenTheActiveLookupFails(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	history := []*models.ChatMessage{{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "q",
		AdditionalContext: []models.AdditionalContextItem{memoryItem(&id, "Some memory")}}}
	b := testBuilderWithLookup(t, history, nil)
	b.activeMemoryLookup = func(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]struct{}, error) {
		return nil, errors.New("db down")
	}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: &models.Chat{ID: uuid.New()}, UserPrompt: "now"})
	require.NoError(t, err)
	require.Contains(t, segmentText(mc, provider.SegmentKindMemoryContext), "Some memory")
}
