package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// TestBuildCandidatesFoldsInSearchLoadedMemories pins that memories pulled in via find_context
// (which land in chatCtx.liveMemories, NOT the frozen MemoryRefs snapshot) become merge candidates,
// while a memory reachable through both paths is not double-counted.
func TestBuildCandidatesFoldsInSearchLoadedMemories(t *testing.T) {
	prefetchedID := uuid.New()
	searchedID := uuid.New()

	// The frozen prefetch snapshot the deduper historically saw.
	mc := &provider.ModelContext{
		MemoryRefs: []provider.ContextMemoryRef{
			{Content: "User prefers dark mode", Scope: "User", MemoryID: prefetchedID.String()},
		},
	}
	// liveMemories: the same prefetched row PLUS one the agent pulled in via find_context.
	live := []*models.Memory{
		{ID: prefetchedID, Content: "User prefers dark mode", Scope: "User", Confidence: 0.6},
		{ID: searchedID, Content: "User is based in Berlin", Scope: "User", Confidence: 0.9},
	}

	candidates := buildMemoryMergeCandidates(mc, live, nil)

	require.Len(t, candidates, 2, "prefetched (deduped) + search-loaded = 2 unique candidates")
	var foundSearched bool
	for _, c := range candidates {
		if c.MemoryID != nil && *c.MemoryID == searchedID {
			foundSearched = true
			require.Equal(t, "User is based in Berlin", c.Content)
			require.Equal(t, models.MemoryConfidenceHigh, c.Confidence, "search-loaded memory keeps its real confidence")
		}
	}
	require.True(t, foundSearched, "a find_context-loaded memory must be a merge/link candidate")
}

// TestLoadedOnlyDuplicatesPlanAFold is the roll-forward guarantee behind the issue-#1 fix: a
// checkpoint that freshly extracted NOTHING but loaded a duplicate cluster (via prefetch /
// find_context) must still produce a fold. Before the fix compactMemoriesFromCheckpoint
// early-returned on empty extraction and these never collapsed. Here extracted is nil, the two
// loaded rows are functional duplicates, and planMemoryCompaction must emit one fold that
// absorbs the second into the first.
func TestLoadedOnlyDuplicatesPlanAFold(t *testing.T) {
	survivorID := uuid.New()
	absorbedID := uuid.New()
	live := []*models.Memory{
		{ID: survivorID, Content: "User prefers dark mode", Scope: "User", Confidence: 0.6},
		{ID: absorbedID, Content: "The user likes dark mode UIs", Scope: "User", Confidence: 0.6},
	}

	// extracted is nil — the whole point: nothing new arrived this checkpoint.
	candidates := buildMemoryMergeCandidates(nil, live, nil)
	require.Len(t, candidates, 2, "both loaded duplicates are candidates even with no fresh extraction")

	// Simulate the persona-driven inference deciding the two loaded rows are one cluster.
	group := models.MemoryMergeGroupProposal{
		MemberIndices:    []int{0, 1},
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: "User prefers dark mode",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}
	require.False(t, noOpGuard(group, candidates), "a two-member loaded dup cluster must not be skipped")

	plan := planMemoryCompaction([]models.MemoryMergeGroupProposal{group}, candidates)
	require.Len(t, plan.Folds, 1, "loaded-only duplicates must still fold")
	require.NotNil(t, plan.Folds[0].SurvivorID)
	require.Equal(t, survivorID, *plan.Folds[0].SurvivorID)
	require.ElementsMatch(t, []uuid.UUID{absorbedID}, plan.Folds[0].AbsorbIDs)
	require.False(t, plan.Folds[0].NeedsEmbedding, "survivor already has an embedding; reuse it")
}

// TestExistingDuplicateClusterSurvivesCandidateDedup pins the defensive dedup rule: distinct
// memory rows (different ids) are NEVER collapsed by the candidate builder — not even when their
// normalized content is identical — because that cluster is exactly what the merger must fold. Only
// the SAME row surfaced through two paths (same id) is deduped. This is the motivating scenario: new
// {A,B,C} alongside a loaded cluster of duplicate D rows; every D must remain a candidate so the
// merger can de-index the extras.
func TestExistingDuplicateClusterSurvivesCandidateDedup(t *testing.T) {
	aID := uuid.New()
	// Four EXACT-content duplicate rows of "D" with DISTINCT ids (the hardest case; near-dupes with
	// distinct wording already survive because their content differs).
	dIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	live := []*models.Memory{{ID: aID, Content: "Fact A", Scope: "User", Confidence: 0.6}}
	for _, id := range dIDs {
		live = append(live, &models.Memory{ID: id, Content: "Fact D", Scope: "User", Confidence: 0.6})
	}
	// New extractions this turn — none of them "D".
	extracted := []memoryutil.CollapsedExtractedMemory{
		{Content: "Fact A", Scope: "User", Confidence: models.MemoryConfidenceMedium, BatchDuplicateCount: 1},
		{Content: "Fact B", Scope: "User", Confidence: models.MemoryConfidenceMedium, BatchDuplicateCount: 1},
		{Content: "Fact C", Scope: "User", Confidence: models.MemoryConfidenceMedium, BatchDuplicateCount: 1},
	}

	candidates := buildMemoryMergeCandidates(nil, live, extracted)

	// All four distinct D rows survive as candidates (not collapsed to one by content).
	dMemberIdx := make([]int, 0, 4)
	seenDIDs := make(map[uuid.UUID]struct{})
	for i, c := range candidates {
		if c.MemoryID != nil {
			if _, ok := seenDIDs[*c.MemoryID]; ok {
				continue
			}
		}
		if c.Content == "Fact D" && c.MemoryID != nil {
			dMemberIdx = append(dMemberIdx, i)
			seenDIDs[*c.MemoryID] = struct{}{}
		}
	}
	require.Len(t, dMemberIdx, 4, "all four distinct D rows must remain foldable candidates")

	// The merger clusters the four D's; the fold must retire three and keep one.
	group := models.MemoryMergeGroupProposal{
		MemberIndices:    dMemberIdx,
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: "Fact D",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}
	plan := planMemoryCompaction([]models.MemoryMergeGroupProposal{group}, candidates)
	require.Len(t, plan.Folds, 1)
	require.NotNil(t, plan.Folds[0].SurvivorID)
	require.Equal(t, dIDs[0], *plan.Folds[0].SurvivorID)
	require.Len(t, plan.Folds[0].AbsorbIDs, 3, "the other three duplicate rows are de-indexed")
}

// noOpGuard mirrors the skip condition in compactMemoriesFromCheckpoint: a merge group is a
// no-op (and would spuriously inflate duplicate_count) when it is a single already-stored
// memory with nothing to fold — i.e. a survivor with no absorbed dupes and no new member.
func noOpGuard(group models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) bool {
	survivorID, absorbIDs := survivorMemoryIDForGroup(group, candidates)
	return survivorID != nil && len(absorbIDs) == 0 && !groupHasNewMember(group, candidates)
}

// TestSlackFiveExistingDuplicatesCollapse encodes the canonical easy case: five
// near-identical Slack-posting-rule memories, all already stored (no fresh extraction this
// turn). With the relaxed trigger they must still fold: one survivor, the other four absorbed.
func TestSlackFiveExistingDuplicatesCollapse(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	// Distinct wording, same rule — exactly the "functional equivalence, not textual" case.
	contents := []string{
		"Post the daily digest as a root message to #status-updates (C0000000001); never use thread_ts; on failure paste the digest in chat.",
		"Slack posting rule: root message to #status-updates only, no thread replies; if it fails, output the digest and note the failure.",
		"Always post the digest to #status-updates (C0000000001) as a root message; do not reply in a thread; surface failures in chat.",
		"The daily digest goes to #status-updates as a ROOT message (no thread_ts); paste digest into chat if Slack posting fails.",
		"Posting rule for #status-updates: root only, never thread_ts; on Slack failure, drop the full digest into chat with a note.",
	}
	candidates := make([]memoryMergeCandidate, 0, len(ids))
	member := make([]int, 0, len(ids))
	for i, id := range ids {
		idCopy := id
		candidates = append(candidates, memoryMergeCandidate{
			Content:    contents[i],
			Scope:      "User",
			Confidence: models.MemoryConfidenceMedium,
			MemoryID:   &idCopy,
		})
		member = append(member, i)
	}
	// Simulates the (persona-driven) inference deciding these are one duplicate cluster.
	group := models.MemoryMergeGroupProposal{
		MemberIndices:    member,
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: contents[0],
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}

	require.False(t, groupHasNewMember(group, candidates), "all five are already-stored, none new")
	require.False(t, noOpGuard(group, candidates), "a five-member dup cluster must NOT be skipped")

	survivor, absorb := survivorMemoryIDForGroup(group, candidates)
	require.NotNil(t, survivor)
	require.Equal(t, ids[0], *survivor, "first candidate with an ID is the survivor")
	require.ElementsMatch(t, ids[1:], absorb, "the other four are absorbed (de-indexed), not the survivor")
}

// TestSingleExistingMemoryIsNoOp guards against the relaxed trigger re-folding a lone memory
// each checkpoint and inflating its own duplicate_count.
func TestSingleExistingMemoryIsNoOp(t *testing.T) {
	id := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Prefers dark mode", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &id},
	}
	group := models.MemoryMergeGroupProposal{
		MemberIndices:    []int{0},
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: "Prefers dark mode",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceHigh,
	}
	require.True(t, noOpGuard(group, candidates), "a lone existing memory has nothing to fold")
}

// TestExistingPlusNewFolds is the classic re-confirmation: a fresh extraction matches an
// existing memory. It must process (fold the new observation in), not be skipped.
func TestExistingPlusNewFolds(t *testing.T) {
	id := uuid.New()
	candidates := []memoryMergeCandidate{
		{Content: "Prefers dark mode", Scope: "User", Confidence: models.MemoryConfidenceHigh, MemoryID: &id},
		{Content: "User likes dark mode", Scope: "User", Confidence: models.MemoryConfidenceMedium, IsNew: true},
	}
	group := models.MemoryMergeGroupProposal{
		MemberIndices:    []int{0, 1},
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: "Prefers dark mode",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceHigh,
	}
	require.True(t, groupHasNewMember(group, candidates))
	require.False(t, noOpGuard(group, candidates))
	survivor, absorb := survivorMemoryIDForGroup(group, candidates)
	require.NotNil(t, survivor)
	require.Equal(t, id, *survivor)
	require.Empty(t, absorb, "the new member folds into the survivor; nothing to absorb/de-index")
}
