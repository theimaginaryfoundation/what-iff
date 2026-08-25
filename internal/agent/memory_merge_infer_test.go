package agent

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// TestSalvageMemoryMergeGroups_Truncated pins the truncation safety net: a grouping response cut off
// mid-array (as happens when the merger blows MaxOutputTokens) still yields every complete group
// object, dropping only the incomplete tail.
func TestSalvageMemoryMergeGroups_Truncated(t *testing.T) {
	// Two complete groups, then a third object cut off mid-string — exactly what a token-limit
	// truncation produces.
	raw := `{"groups":[` +
		`{"member_indices":[0],"relation":"merge","canonical_content":"","scope":"User","confidence":"medium"},` +
		`{"member_indices":[1,2],"relation":"merge","canonical_content":"Folded fact","scope":"User","confidence":"high"},` +
		`{"member_indices":[3],"relation":"merge","canonical_content":"never fini`

	groups := salvageMemoryMergeGroups(raw)
	require.Len(t, groups, 2, "the two complete groups survive; the truncated third is dropped")
	require.Equal(t, []int{0}, groups[0].MemberIndices)
	require.Equal(t, []int{1, 2}, groups[1].MemberIndices)
	require.Equal(t, "Folded fact", groups[1].CanonicalContent)
}

func TestSalvageMemoryMergeGroups_NotJSON(t *testing.T) {
	require.Nil(t, salvageMemoryMergeGroups("totally not json"))
	require.Nil(t, salvageMemoryMergeGroups(""))
}

// TestBackfillCanonicalContent fills singleton canonical_content (now emitted empty to shrink
// output) from the member, since persistence drops empty-content groups.
func TestBackfillCanonicalContent(t *testing.T) {
	id := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Prefers dark mode", Scope: "User", MemoryID: &id},
		{Content: "Drinks tea", Scope: "User", IsNew: true},
	}
	groups := []models.MemoryMergeGroupProposal{
		{MemberIndices: []int{0}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "", Scope: "User"},
		{MemberIndices: []int{1}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "Explicit canonical", Scope: "User"},
	}
	backfillCanonicalContent(groups, candidates)
	require.Equal(t, "Prefers dark mode", groups[0].CanonicalContent, "empty singleton canonical is filled from its member")
	require.Equal(t, "Explicit canonical", groups[1].CanonicalContent, "non-empty canonical is left untouched")
}

// TestEnsureAllCandidatesCovered guarantees a candidate the inference omitted (e.g. dropped by a
// salvaged truncation) is still processed — critical for a freshly extracted memory that must be
// created rather than silently lost.
func TestEnsureAllCandidatesCovered(t *testing.T) {
	existingID := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Existing covered", Scope: "User", MemoryID: &existingID},
		{Content: "New dropped by truncation", Scope: "User", IsNew: true},
	}
	// The inference only returned a group for index 0; index 1 (a new memory) was dropped.
	groups := []models.MemoryMergeGroupProposal{
		{MemberIndices: []int{0}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "Existing covered", Scope: "User"},
	}
	covered := ensureAllCandidatesCovered(groups, candidates)
	require.Len(t, covered, 2, "a group is added for the uncovered new candidate")

	var sawNew bool
	for _, g := range covered {
		for _, idx := range g.MemberIndices {
			if idx == 1 {
				sawNew = true
				require.Equal(t, "New dropped by truncation", g.CanonicalContent)
			}
		}
	}
	require.True(t, sawNew, "the dropped new memory is now covered so it will still be created")
}

// TestEnsureAllCandidatesCovered_NoGaps is a no-op when everything is already covered.
func TestEnsureAllCandidatesCovered_NoGaps(t *testing.T) {
	candidates := []memoryMergeCandidate{{Content: "a", Scope: "User"}, {Content: "b", Scope: "User"}}
	groups := []models.MemoryMergeGroupProposal{
		{MemberIndices: []int{0, 1}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "a", Scope: "User"},
	}
	require.Len(t, ensureAllCandidatesCovered(groups, candidates), 1)
}

// TestSalvageScalesToManyGroups is a light guard that salvage handles a large complete array.
func TestSalvageScalesToManyGroups(t *testing.T) {
	raw := `{"groups":[`
	for i := 0; i < 100; i++ {
		if i > 0 {
			raw += ","
		}
		raw += fmt.Sprintf(`{"member_indices":[%d],"relation":"merge","canonical_content":"","scope":"User","confidence":"medium"}`, i)
	}
	raw += `]}`
	require.Len(t, salvageMemoryMergeGroups(raw), 100)
}

func TestSourceMembersForGroup(t *testing.T) {
	liveID := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Prefers dark mode", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &liveID},
		{Content: "User likes dark mode", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
	}
	group := models.MemoryMergeGroupProposal{
		MemberIndices:    []int{0, 1},
		CanonicalContent: "Prefers dark mode",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}

	members := sourceMembersForGroup(group, candidates)
	require.Len(t, members, 2)
	require.Equal(t, liveID, *members[0].MemoryID)
	require.False(t, members[0].IsNew)
	require.True(t, members[1].IsNew)
	require.Equal(t, "User likes dark mode", members[1].Content)
}

func TestPlanMemoryCompaction_PartitionsMergeAndLink(t *testing.T) {
	liveA := uuid.New()
	liveB := uuid.New()
	liveC := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Prefers dark mode", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &liveA},
		{Content: "Likes dark UI", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
		{Content: "Incident: pager woke me", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &liveB},
		{Content: "Felt exhausted after the page", Scope: "User", Confidence: models.MemoryConfidenceMedium, MemoryID: &liveC},
		{Content: "Already stored singleton", Scope: "Chat", Confidence: models.MemoryConfidenceMedium, MemoryID: &liveA},
	}

	groups := []models.MemoryMergeGroupProposal{
		{
			MemberIndices:    []int{0, 1},
			Relation:         models.MemoryMergeRelationMerge,
			CanonicalContent: "Prefers dark mode",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceHigh,
		},
		{
			MemberIndices:    []int{2, 3},
			Relation:         models.MemoryMergeRelationLink,
			CanonicalContent: "on-call incident",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceHigh,
		},
		{
			MemberIndices:    []int{4},
			Relation:         models.MemoryMergeRelationMerge,
			CanonicalContent: "Already stored singleton",
			Scope:            "Chat",
			Confidence:       models.MemoryConfidenceMedium,
		},
	}

	plan := planMemoryCompaction(groups, candidates)

	require.Len(t, plan.Folds, 1, "singleton already-stored merge should be filtered as no-op")
	require.Equal(t, "Prefers dark mode", plan.Folds[0].Group.CanonicalContent)
	require.NotNil(t, plan.Folds[0].SurvivorID)
	require.Equal(t, liveA, *plan.Folds[0].SurvivorID)
	require.Empty(t, plan.Folds[0].AbsorbIDs)
	require.False(t, plan.Folds[0].NeedsEmbedding)
	require.Equal(t, 2, plan.Folds[0].DuplicatesFolded)

	require.Len(t, plan.Links, 1)
	require.Equal(t, "on-call incident", plan.Links[0].Group.CanonicalContent)
	require.ElementsMatch(t, []uuid.UUID{liveB, liveC}, plan.Links[0].ExistingIDs)
	require.Empty(t, plan.Links[0].NewMembers)
}

func TestPlanMemoryCompaction_LinkWithNewMembers(t *testing.T) {
	liveID := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Technical: retry with backoff", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &liveID},
		{Content: "Emotional: felt stuck debugging", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
	}
	groups := []models.MemoryMergeGroupProposal{
		{
			MemberIndices:    []int{0, 1},
			Relation:         models.MemoryMergeRelationLink,
			CanonicalContent: "debug stubbornness",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceHigh,
		},
	}

	plan := planMemoryCompaction(groups, candidates)
	require.Empty(t, plan.Folds)
	require.Len(t, plan.Links, 1)
	require.Equal(t, []uuid.UUID{liveID}, plan.Links[0].ExistingIDs)
	require.Len(t, plan.Links[0].NewMembers, 1)
	require.Equal(t, "Emotional: felt stuck debugging", plan.Links[0].NewMembers[0].Content)
}

func TestPlanMemoryCompaction_CreateNeedsEmbedding(t *testing.T) {
	candidates := []memoryMergeCandidate{
		{Content: "Likes tea", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
		{Content: "Prefers tea over coffee", Scope: "User", Confidence: models.MemoryConfidenceHigh, IsNew: true},
	}
	groups := []models.MemoryMergeGroupProposal{
		{
			MemberIndices:    []int{0, 1},
			Relation:         models.MemoryMergeRelationMerge,
			CanonicalContent: "Prefers tea over coffee",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceHigh,
		},
	}

	plan := planMemoryCompaction(groups, candidates)
	require.Len(t, plan.Folds, 1)
	require.Nil(t, plan.Folds[0].SurvivorID)
	require.True(t, plan.Folds[0].NeedsEmbedding)
	require.Empty(t, plan.Links)
}

// Singleton new extractions still plan as folds so PersistMemoryMergeGroup can create the row and
// attach it to compaction.created_memories (no merge event).
func TestStandaloneNewExtractionStillPlansAsFold(t *testing.T) {
	plan := planMemoryCompaction([]models.MemoryMergeGroupProposal{
		{
			MemberIndices:    []int{0},
			Relation:         models.MemoryMergeRelationMerge,
			CanonicalContent: "Prefers tea",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceMedium,
		},
	}, []memoryMergeCandidate{
		{Content: "Prefers tea", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
	})

	require.Len(t, plan.Folds, 1)
	require.Nil(t, plan.Folds[0].SurvivorID)
	require.Empty(t, plan.Folds[0].AbsorbIDs)
	require.Len(t, plan.Folds[0].SourceMembers, 1)
	require.True(t, plan.Folds[0].SourceMembers[0].IsNew)
	require.Equal(t, 1, plan.Folds[0].DuplicatesFolded)
}

func TestPlanMemoryCompaction_LinkDropsCrossScopeAndOutOfRange(t *testing.T) {
	liveID := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "User fact", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &liveID},
		{Content: "Chat fact", Scope: "Chat", Confidence: models.MemoryConfidenceMedium, IsNew: true},
	}
	groups := []models.MemoryMergeGroupProposal{
		{
			MemberIndices:    []int{0, 1},
			Relation:         models.MemoryMergeRelationLink,
			CanonicalContent: "mixed scopes",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceHigh,
		},
		{
			MemberIndices:    []int{99, 100},
			Relation:         models.MemoryMergeRelationLink,
			CanonicalContent: "all invalid",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceMedium,
		},
	}

	plan := planMemoryCompaction(groups, candidates)
	require.Empty(t, plan.Folds)
	require.Empty(t, plan.Links, "cross-scope and out-of-range link groups should be dropped")
}

func TestPlanMemoryCompaction_LinkAllNewMembers(t *testing.T) {
	candidates := []memoryMergeCandidate{
		{Content: "Technical angle", Scope: "User", Confidence: models.MemoryConfidenceHigh, IsNew: true},
		{Content: "Emotional angle", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
	}
	groups := []models.MemoryMergeGroupProposal{
		{
			MemberIndices:    []int{0, 1},
			Relation:         models.MemoryMergeRelationLink,
			CanonicalContent: "same event two angles",
			Scope:            "User",
			Confidence:       models.MemoryConfidenceHigh,
		},
	}

	plan := planMemoryCompaction(groups, candidates)
	require.Empty(t, plan.Folds)
	require.Len(t, plan.Links, 1)
	require.Empty(t, plan.Links[0].ExistingIDs)
	require.Len(t, plan.Links[0].NewMembers, 2)
}
