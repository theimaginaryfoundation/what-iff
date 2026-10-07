package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func relayChatCtx() *chatContext {
	return &chatContext{
		chat:             &models.Chat{ID: uuid.New(), UserID: uuid.New(), ContextScope: models.ContextScopeSandbox},
		memoryProvenance: models.MemoryProvenanceExternal,
	}
}

func ownChatCtx() *chatContext {
	return &chatContext{chat: &models.Chat{ID: uuid.New(), UserID: uuid.New()}}
}

func TestRelayThreadExtractionUsesTheSpeakerSchemaAndInstructions(t *testing.T) {
	relay, own := relayChatCtx(), ownChatCtx()

	props := func(schema map[string]interface{}) map[string]interface{} {
		items := schema["properties"].(map[string]interface{})["memories"].(map[string]interface{})["items"].(map[string]interface{})
		return items["properties"].(map[string]interface{})
	}
	assert.Contains(t, props(memoryExtractionSchemaFor(relay)), "speaker")
	assert.NotContains(t, props(memoryExtractionSchemaFor(own)), "speaker", "ordinary chats keep the default schema")
	claude := extractionSchemaCopy(memoryExtractionSchemaFor(relay))
	claude["mutated"] = true
	assert.NotContains(t, memoryExtractionSchemaFor(relay), "mutated", "providers get a copy")

	assert.Contains(t, memoryExtractionDeveloperNote(relay), "NOT the account owner")
	assert.NotContains(t, memoryExtractionDeveloperNote(own), "Discord")

	raw, err := json.Marshal(models.ExtractedRelayMemoryResponse{Memories: []models.ExtractedRelayMemory{
		{Content: "alice likes tea", Scope: "User", Confidence: "high", Speaker: "alice"},
	}})
	require.NoError(t, err)
	got, err := decodeExtractedMemories(relay, raw)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "alice", got[0].Speaker)

	got, err = decodeExtractedMemories(own, raw)
	require.NoError(t, err)
	assert.Equal(t, "", got[0].Speaker, "a speaker means nothing outside a relay thread")
}

func TestExtractedMemoriesAreExternalOnlyInARelayThread(t *testing.T) {
	in := func() []memoryutil.CollapsedExtractedMemory {
		return []memoryutil.CollapsedExtractedMemory{{Content: "a", Scope: "User", Speaker: "alice"}}
	}
	ext := stampExtractedProvenance(in(), true)
	assert.Equal(t, models.MemoryOrigin{Provenance: models.MemoryProvenanceExternal, Speaker: "alice"}, ext[0].Origin())
	own := stampExtractedProvenance(in(), false)
	assert.Equal(t, models.MemoryOrigin{Provenance: models.MemoryProvenanceUser}, own[0].Origin())
}

func TestCompactionPlansCarryTheNewMembersOrigin(t *testing.T) {
	stored := uuid.New()
	alice := models.MemoryOrigin{Provenance: models.MemoryProvenanceExternal, Speaker: "alice"}
	bob := models.MemoryOrigin{Provenance: models.MemoryProvenanceExternal, Speaker: "bob"}
	candidates := []memoryMergeCandidate{
		{Content: "likes tea", Scope: "User", MemoryID: &stored},
		{Content: "likes tea", Scope: "User", IsNew: true, Origin: alice},
		{Content: "likes green tea", Scope: "User", IsNew: true, Origin: alice},
		{Content: "has a cat", Scope: "User", IsNew: true, Origin: alice},
		{Content: "has a cat", Scope: "User", IsNew: true, Origin: bob},
	}
	plan := planMemoryCompaction([]models.MemoryMergeGroupProposal{
		{MemberIndices: []int{0, 1, 2}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "likes tea", Scope: "User"},
		{MemberIndices: []int{3, 4}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "has a cat", Scope: "User"},
	}, candidates)
	require.Len(t, plan.Folds, 2)
	require.NotNil(t, plan.Folds[0].NewOrigin)
	assert.Equal(t, alice, *plan.Folds[0].NewOrigin, "the stored survivor's origin is the datastore's to combine")
	assert.Equal(t, models.MemoryOrigin{Provenance: models.MemoryProvenanceExternal}, *plan.Folds[1].NewOrigin, "two speakers: none kept")
	assert.Len(t, foldMemberOptions(plan.Folds[0], false), 1, "the new members' origin")
	assert.Len(t, foldMemberOptions(plan.Folds[0], true), 2, "plus the chat-memories-only guard of a sandbox")

	storedOnly := planFoldGroupForTest(t, candidates)
	assert.Nil(t, storedOnly.NewOrigin, "a group without new members has no origin of its own")

	link := planMemoryCompaction([]models.MemoryMergeGroupProposal{
		{MemberIndices: []int{0, 3}, Relation: models.MemoryMergeRelationLink, CanonicalContent: "pets", Scope: "User"},
	}, candidates)
	require.Len(t, link.Links, 1)
	require.Len(t, link.Links[0].NewMembers, 1)
	assert.Equal(t, alice, link.Links[0].NewMembers[0].Origin)
}

func planFoldGroupForTest(t *testing.T, candidates []memoryMergeCandidate) memoryFoldPlan {
	t.Helper()
	other := uuid.New()
	cands := append([]memoryMergeCandidate{}, candidates[0], memoryMergeCandidate{Content: "likes tea", Scope: "User", MemoryID: &other})
	fold, ok := planFoldGroup(models.MemoryMergeGroupProposal{MemberIndices: []int{0, 1}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "likes tea", Scope: "User"}, cands)
	require.True(t, ok)
	return fold
}

func TestCreateMemoryOriginFollowsTheChat(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	ctx := context.Background()

	assert.Equal(t, models.MemoryOrigin{Provenance: models.MemoryProvenanceUser}, a.createMemoryOrigin(ctx, ownChatCtx()))
	assert.Equal(t, models.MemoryOrigin{Provenance: models.MemoryProvenanceUser}, a.createMemoryOrigin(ctx, nil))
	relay := relayChatCtx()
	relay.triggerMessageID = uuid.New()
	assert.Equal(t, models.MemoryOrigin{Provenance: models.MemoryProvenanceExternal}, a.createMemoryOrigin(ctx, relay), "no store: external, speaker unknown")

	// Without a store or a chat nothing can be a relay thread.
	assert.Equal(t, models.MemoryProvenanceUser, a.memoryProvenanceForChat(ctx, uuid.New(), &models.Chat{ID: uuid.New()}))
	assert.Equal(t, models.MemoryProvenanceUser, a.memoryProvenanceForChat(ctx, uuid.New(), &models.Chat{}))
	assert.Equal(t, models.MemoryProvenanceUser, a.memoryProvenanceForChat(ctx, uuid.New(), nil))
}
