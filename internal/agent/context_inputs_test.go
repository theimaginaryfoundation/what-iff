package agent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

func TestContextInputs_RecordsStagesReplayAndVersions(t *testing.T) {
	rel := 0.82
	prefetched := &models.Memory{ID: uuid.New(), Scope: "User", Relevance: &rel}
	alsoPrefetched := &models.Memory{ID: uuid.New(), Scope: "Chat"}
	fromTool := &models.Memory{ID: uuid.New(), Scope: "User"}
	replayed := uuid.New().String()
	mood := &models.Mood{ID: uuid.New()}

	chatCtx := &chatContext{
		chat:                  &models.Chat{Scratchpad: "notes v1", CheckpointSummary: "summary v3"},
		liveMemories:          []*models.Memory{prefetched, alsoPrefetched, fromTool, prefetched},
		prefetchedMemoryCount: 2,
		activeMood:            mood,
	}
	modelContext := &provider.ModelContext{MemoryRefs: []provider.ContextMemoryRef{
		{MemoryID: prefetched.ID.String()}, // already counted this turn
		{MemoryID: replayed},
		{MemoryID: " "},
	}}

	in := contextInputs(chatCtx, modelContext)
	require.NotNil(t, in)
	require.Len(t, in.Memories, 3, "duplicates are recorded once")
	assert.Equal(t, modeltypes.ContextMemoryInput{ID: prefetched.ID.String(), Scope: "User", Stage: "prefetch", Relevance: &rel}, in.Memories[0])
	assert.Equal(t, "prefetch", in.Memories[1].Stage)
	assert.Equal(t, "tool", in.Memories[2].Stage)
	assert.Equal(t, []string{replayed}, in.ReplayedMemoryIDs)
	assert.Equal(t, mood.ID.String(), in.MoodID)
	assert.Len(t, in.ScratchpadSHA, contextInputsHashLen)
	assert.NotEqual(t, in.ScratchpadSHA, in.SummarySHA)
	assert.Equal(t, shortContentHash("notes v1"), in.ScratchpadSHA, "the hash is stable for the same content")

	raw, err := json.Marshal(in)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "notes v1", "the manifest never carries content")
	assert.NotContains(t, string(raw), "summary v3")
}

func TestContextInputs_EmptyAndFailure(t *testing.T) {
	assert.Nil(t, contextInputs(nil, nil))
	assert.Nil(t, contextInputs(&chatContext{chat: &models.Chat{}}, nil), "nothing to record")

	failed := contextInputs(&chatContext{chat: &models.Chat{}, memoryEnrichmentFailed: true}, nil)
	require.NotNil(t, failed)
	assert.True(t, failed.MemoryEnrichmentFailed)
}

func TestBuildContextBreakdown_CarriesInputs(t *testing.T) {
	a := &Agent{}
	mem := &models.Memory{ID: uuid.New(), Scope: "User"}
	chatCtx := &chatContext{chat: &models.Chat{}, liveMemories: []*models.Memory{mem}, prefetchedMemoryCount: 1}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindMemoryContext, provider.RoleDeveloper, "a memory line", false)

	breakdown := a.buildContextBreakdown(chatCtx, mc, 0, nil)
	require.NotNil(t, breakdown)
	require.NotNil(t, breakdown.Inputs)
	require.Len(t, breakdown.Inputs.Memories, 1)
	assert.Equal(t, mem.ID.String(), breakdown.Inputs.Memories[0].ID)
}
