package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestNormalizeMemoryScope(t *testing.T) {
	require.Equal(t, "User", normalizeMemoryScope("User"))
	require.Equal(t, "Chat", normalizeMemoryScope("chat"))
	require.Equal(t, "", normalizeMemoryScope("summary"))
}

func TestFilterGroupsByScopeConsistency_RejectsMixedScope(t *testing.T) {
	id := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "likes tea", Scope: "User", IsNew: true},
		{Content: "likes tea", Scope: "Chat", MemoryID: &id},
	}
	groups := []models.MemoryMergeGroupProposal{{
		MemberIndices:    []int{0, 1},
		CanonicalContent: "likes tea",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}}
	filtered := filterGroupsByScopeConsistency(groups, candidates)
	require.Empty(t, filtered)
}
