package memoryutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestCollapseExtractedMemories_DedupesByContentAndScope(t *testing.T) {
	collapsed := CollapseExtractedMemories([]models.ExtractedMemory{
		{Content: "  Likes tea ", Scope: "User", Confidence: models.MemoryConfidenceHigh},
		{Content: "likes tea", Scope: "User", Confidence: models.MemoryConfidenceLow},
		{Content: "likes tea", Scope: "Chat", Confidence: models.MemoryConfidenceMedium},
		{Content: "Other fact", Scope: "Chat", Confidence: models.MemoryConfidenceMedium},
	})

	require.Len(t, collapsed, 3)

	byContent := map[string]CollapsedExtractedMemory{}
	for _, item := range collapsed {
		byContent[item.Content+"|"+item.Scope] = item
	}

	userTea := byContent["Likes tea|User"]
	require.Equal(t, 2, userTea.BatchDuplicateCount)
	require.Equal(t, models.MemoryConfidenceLow, userTea.Confidence)

	chatTea := byContent["likes tea|Chat"]
	require.Equal(t, 1, chatTea.BatchDuplicateCount)
}

func TestCollapseExtractedMemories_EmptyInput(t *testing.T) {
	require.Nil(t, CollapseExtractedMemories(nil))
	require.Nil(t, CollapseExtractedMemories([]models.ExtractedMemory{{Content: "   "}}))
}
