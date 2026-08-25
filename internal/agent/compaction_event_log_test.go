package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestLoadedMemoriesSnapshotIncludesFullSegmentMemorySet(t *testing.T) {
	historicalAID := uuid.New()
	historicalBID := uuid.New()
	currentOnlyID := uuid.New()
	modelContext := &provider.ModelContext{
		MemoryRefs: []provider.ContextMemoryRef{
			{Content: "User prefers dark mode", Scope: "User", MemoryID: historicalAID.String()},
			{Content: "User is based in Berlin", Scope: "User", MemoryID: historicalBID.String()},
		},
	}
	liveMemories := []*models.Memory{
		// This current-turn prefetch duplicates a memory persisted from an earlier turn.
		{ID: historicalAID, Content: "User prefers dark mode", Scope: "User", Confidence: 0.9},
		// Tool-loaded memory is not present in the frozen context snapshot.
		{ID: currentOnlyID, Content: "User uses Neovim", Scope: "User", Confidence: 0.6},
	}

	snapshot := loadedMemoriesSnapshot(modelContext, liveMemories)

	require.Len(t, snapshot, 3)
	byID := make(map[uuid.UUID]models.CompactionLoadedMemory, len(snapshot))
	for _, memory := range snapshot {
		require.NotNil(t, memory.MemoryID)
		byID[*memory.MemoryID] = memory
	}
	require.Equal(t, "User prefers dark mode", byID[historicalAID].Content)
	require.Equal(t, "User is based in Berlin", byID[historicalBID].Content)
	require.Equal(t, "User uses Neovim", byID[currentOnlyID].Content)
}
