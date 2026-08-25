package agent

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestFormatMemoryForContext(t *testing.T) {
	created := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mem := &models.Memory{
		Content:    "Prefers dark mode",
		CreatedAt:  created,
		Confidence: 0.9,
	}
	formatted := formatMemoryForContext(mem)
	require.Contains(t, formatted, "Prefers dark mode")
	require.Contains(t, formatted, "stored_at=2024-06-01T12:00:00Z")
	require.Contains(t, formatted, "confidence=0.90")
	require.Contains(t, formatted, "age_days=")
	// No chain metadata => no reconfirmation noise for an ordinary one-off memory.
	require.NotContains(t, formatted, "reconfirmed")
}

func TestFormatMemoryForContext_SurfacesReconfirmationTally(t *testing.T) {
	mem := &models.Memory{
		Content:       "Post the daily digest as a ROOT message to #status-updates (C0000000001)",
		CreatedAt:     time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Confidence:    0.9,
		ChainMetadata: &models.MemoryChainMetadata{DuplicateCount: 5},
	}
	formatted := formatMemoryForContext(mem)
	// A fact independently arrived at 5 times reads as corroborated to the agent.
	require.Contains(t, formatted, "reconfirmed=5x")
	// And it stays inside the [...] block so metadata stripping still removes it cleanly.
	require.Equal(t, mem.Content, memoryutil.StripMemoryContextMetadata(formatted))
}

func TestAddLoadedMemories_PersistsForAdditionalContext(t *testing.T) {
	id := uuid.New()
	mem := &models.Memory{
		ID:         id,
		Content:    "Prefers dark mode",
		Scope:      "User",
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC(),
	}
	chatCtx := &chatContext{
		memories:     []string{"The user's name is Alice"},
		liveMemories: nil,
	}
	chatCtx.addLoadedMemories([]*models.Memory{mem})

	require.Len(t, chatCtx.liveMemories, 1)
	require.True(t, chatCtx.hasMemoryStalenessNote())

	items := additionalContextItemsFromChatContext(chatCtx)
	require.Len(t, items, 2)

	var memItem *models.AdditionalContextItem
	for i := range items {
		if items[i].MemoryID != nil {
			memItem = &items[i]
		}
	}
	require.NotNil(t, memItem)
	require.Equal(t, id, *memItem.MemoryID)
	require.Equal(t, "User", memItem.Scope)
}

func TestAddLoadedMemories_DedupesByID(t *testing.T) {
	id := uuid.New()
	mem := &models.Memory{
		ID:        id,
		Content:   "fact",
		Scope:     "User",
		CreatedAt: time.Now().UTC(),
	}
	chatCtx := &chatContext{
		memories:     []string{memoryStalenessNote, formatMemoryForContext(mem)},
		liveMemories: []*models.Memory{mem},
	}
	chatCtx.addLoadedMemories([]*models.Memory{mem})

	require.Len(t, chatCtx.liveMemories, 1)
	require.Len(t, chatCtx.memories, 2)
}

func TestAddLoadedMemories_SkipsNilAndEmpty(t *testing.T) {
	chatCtx := &chatContext{}
	chatCtx.addLoadedMemories([]*models.Memory{nil, &models.Memory{Content: "   "}})
	require.Empty(t, chatCtx.memories)
	require.Empty(t, chatCtx.liveMemories)
}
