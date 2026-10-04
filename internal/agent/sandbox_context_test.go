package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Restricted-chat context assembly: persisted memory replay, tool-result replay, the scratchpad,
// the checkpoint plan, the manifest, extraction and merge sensitivity.

func memoryItem(id *uuid.UUID, content string) models.AdditionalContextItem {
	return models.AdditionalContextItem{Type: models.AdditionalContextTypeMemory, Content: content, MemoryID: id, Scope: "User"}
}

func testBuilderWithLookup(t *testing.T, history []*models.ChatMessage, lookup memoryIDLookup) *messageContextBuilder {
	t.Helper()
	b, err := newMessageContextBuilder(nil, &telemetry.Telemetry{Logger: zap.NewNop()}, nil,
		func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, *time.Time, string) []*models.ChatMessage {
			return history
		}, nil)
	require.NoError(t, err)
	b.memoryIDLookup = lookup
	return b
}

func segmentText(mc *provider.ModelContext, kind provider.ModelContextSegmentKind) string {
	var parts []string
	for _, s := range mc.Segments {
		if s.Kind == kind {
			parts = append(parts, s.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func TestBuild_RestrictedChatRefiltersReplayedMemoriesByCurrentLimit(t *testing.T) {
	t.Parallel()
	pubID, sensID := uuid.New(), uuid.New()
	history := []*models.ChatMessage{{
		ID: uuid.New(), Origin: models.MessageOriginUser, Message: "earlier question",
		AdditionalContext: []models.AdditionalContextItem{
			memoryItem(&pubID, "A public fact"),
			memoryItem(&sensID, "A sensitive diagnosis loaded back when the limit was higher"),
			memoryItem(nil, "A memory line whose id was never matched"),
			memoryItem(nil, "The user's name is Ada"),
			{Type: "FILE_HINT", Content: "not a memory"},
		},
	}}
	var asked []uuid.UUID
	var askedLimit models.MemorySensitivity
	b := testBuilderWithLookup(t, history, func(_ context.Context, _ uuid.UUID, ids []uuid.UUID, limit models.MemorySensitivity) (map[uuid.UUID]struct{}, error) {
		asked, askedLimit = ids, limit
		return map[uuid.UUID]struct{}{pubID: {}}, nil // the datastore would drop the sensitive one
	})

	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "p", MemorySensitivityLimit: models.MemorySensitivityPersonal}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)

	mem := segmentText(mc, provider.SegmentKindMemoryContext)
	require.Contains(t, mem, "A public fact")
	require.Contains(t, mem, "The user's name is Ada", "profile data is not a stored memory")
	require.NotContains(t, mem, "sensitive diagnosis", "a memory above the CURRENT limit does not replay")
	require.NotContains(t, mem, "never matched", "an unclassifiable memory line fails closed")
	require.Equal(t, models.MemorySensitivityPersonal, askedLimit)
	require.ElementsMatch(t, []uuid.UUID{pubID, sensID}, asked, "one lookup covers every persisted id")
	require.Contains(t, segmentText(mc, provider.SegmentKindDeveloperContext), "not a memory", "non-memory context is untouched")
	for _, ref := range mc.MemoryRefs {
		require.NotEqual(t, sensID.String(), ref.MemoryID)
	}

	// Unrestricted control: same history, no lookup, everything replays.
	asked = nil
	open := &models.Chat{ID: chat.ID, SystemPrompt: "p"}
	mc, err = b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: open, UserPrompt: "now"})
	require.NoError(t, err)
	mem = segmentText(mc, provider.SegmentKindMemoryContext)
	require.Contains(t, mem, "sensitive diagnosis")
	require.Contains(t, mem, "never matched")
	require.Nil(t, asked, "an unrestricted chat never pays for the lookup")
}

func TestBuild_RestrictedChatFailsClosedWhenTheLookupFails(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	history := []*models.ChatMessage{{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "q",
		AdditionalContext: []models.AdditionalContextItem{memoryItem(&id, "Some memory")}}}
	b := testBuilderWithLookup(t, history, func(context.Context, uuid.UUID, []uuid.UUID, models.MemorySensitivity) (map[uuid.UUID]struct{}, error) {
		return nil, errors.New("db down")
	})
	chat := &models.Chat{ID: uuid.New(), MemorySensitivityLimit: models.MemorySensitivityPublic}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err, "a failed re-check must not fail the turn")
	require.NotContains(t, segmentText(mc, provider.SegmentKindMemoryContext), "Some memory")
}

func TestBuild_RestrictedChatCurrentTurnMemoriesAreNotRefiltered(t *testing.T) {
	t.Parallel()
	// This turn's prefetch was filtered in SQL when it was retrieved; the replay filter must not
	// drop it just because the line carries no persisted id.
	b := testBuilderWithLookup(t, nil, func(context.Context, uuid.UUID, []uuid.UUID, models.MemorySensitivity) (map[uuid.UUID]struct{}, error) {
		return map[uuid.UUID]struct{}{}, nil
	})
	chat := &models.Chat{ID: uuid.New(), MemorySensitivityLimit: models.MemorySensitivityPublic}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now", Memories: []string{"fresh prefetched memory"}})
	require.NoError(t, err)
	require.Contains(t, segmentText(mc, provider.SegmentKindMemoryContext), "fresh prefetched memory")
}

func TestBuild_RestrictedChatGetsNoScratchpadOrAccountDataToolReplay(t *testing.T) {
	t.Parallel()
	history := []*models.ChatMessage{
		{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "look something up"},
		{
			ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "done",
			ToolCalls: []*models.ToolCall{
				{ToolName: agenttools.RecallToolSpec.Name, ToolOutput: "find_context result: the user's cardiology notes"},
				{ToolName: agenttools.ListToolSpec.Name, ToolOutput: "list result: past conversations"},
				{ToolName: agenttools.RunSubagentToolSpec.Name, ToolOutput: "subagent result written with the scratchpad"},
				{ToolName: agenttools.ToolNameRecallEntity, ToolOutput: "entity card for John"},
				{ToolName: agenttools.ToolNameReadFile, ToolOutput: "read_file text of agent/journal.md"},
				{ToolName: agenttools.ToolNameGrepFiles, ToolOutput: "grep hit in agent/journal.md"},
				{ToolName: agenttools.GenerateImageToolSpec.Name, ToolOutput: "image generated: a fox"},
			},
		},
	}
	b := testBuilderWithLookup(t, history, nil)

	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "p", Scratchpad: "PERSONALITY SCRATCHPAD", MemorySensitivityLimit: models.MemorySensitivityPublic}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)
	var all strings.Builder
	for _, s := range mc.Segments {
		all.WriteString(s.Content + "\n")
	}
	text := all.String()
	require.NotContains(t, text, "PERSONALITY SCRATCHPAD", "belt and braces: even a Chat that carries a scratchpad does not inject it")
	for _, leaked := range []string{"cardiology", "past conversations", "written with the scratchpad", "entity card for John", "agent/journal.md"} {
		require.NotContains(t, text, leaked)
	}
	require.Contains(t, text, "a fox", "tool results that read no account data replay as before")

	// Unrestricted control: the scratchpad and the tool results are there.
	open := &models.Chat{ID: chat.ID, SystemPrompt: "p", Scratchpad: "PERSONALITY SCRATCHPAD"}
	mc, err = b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: open, UserPrompt: "now"})
	require.NoError(t, err)
	all.Reset()
	for _, s := range mc.Segments {
		all.WriteString(s.Content + "\n")
	}
	require.Contains(t, all.String(), "PERSONALITY SCRATCHPAD")
	require.Contains(t, all.String(), "cardiology")
}

func TestWithoutAccountDataToolResults_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	keep := &models.ToolCall{ToolName: agenttools.GenerateImageToolSpec.Name}
	drop := &models.ToolCall{ToolName: agenttools.RecallToolSpec.Name}
	in := []*models.ChatMessage{{ToolCalls: []*models.ToolCall{drop, keep}}, nil, {}}
	out := withoutAccountDataToolResults(in)
	require.Len(t, out, 3)
	require.Equal(t, []*models.ToolCall{keep}, out[0].ToolCalls)
	require.Len(t, in[0].ToolCalls, 2, "the stored message keeps its calls")
}

func TestCheckpointSteps(t *testing.T) {
	t.Parallel()
	persona := uuid.New()
	cases := []struct {
		name                            string
		chat                            *models.Chat
		scratchpadWritten               bool
		wantScratchpad, wantExtractions bool
	}{
		{"unrestricted with personality runs scratchpad, extracts after it succeeds", &models.Chat{PersonalityID: persona}, true, true, true},
		{"unrestricted waits for the scratchpad delta", &models.Chat{PersonalityID: persona}, false, true, false},
		{"unrestricted without a personality has neither step", &models.Chat{}, false, false, false},
		{"restricted skips the scratchpad but still extracts", &models.Chat{PersonalityID: persona, MemorySensitivityLimit: models.MemorySensitivityPublic}, false, false, true},
		{"restricted without a personality still extracts", &models.Chat{MemorySensitivityLimit: models.MemorySensitivityPersonal}, false, false, true},
	}
	for _, tc := range cases {
		scratch, extract := checkpointSteps(tc.chat, tc.scratchpadWritten)
		assert.Equal(t, tc.wantScratchpad, scratch, tc.name)
		assert.Equal(t, tc.wantExtractions, extract, tc.name)
	}
}

func TestBuildTurnToolPolicy_RestrictedChatLosesTheScratchpadTool(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	policyFor := func(limit models.MemorySensitivity) turnToolPolicy {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true, MemorySensitivityLimit: limit}
		return a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat}, chat.UserID, &models.ChatMessage{})
	}
	require.True(t, policyFor(models.MemorySensitivityPersonal).disabledTools[agenttools.UpdateScratchpadToolSpec.Name])
	require.True(t, policyFor(models.MemorySensitivityPublic).disabledTools[agenttools.UpdateScratchpadToolSpec.Name])
	require.False(t, policyFor("").disabledTools[agenttools.UpdateScratchpadToolSpec.Name])
	require.False(t, policyFor(models.MemorySensitivitySensitive).disabledTools[agenttools.UpdateScratchpadToolSpec.Name])
}

func TestContextInputs_RecordsTheChatLimit(t *testing.T) {
	t.Parallel()
	// A restricted chat always records, even with nothing else to report, so the limit is on file.
	in := contextInputs(&chatContext{chat: &models.Chat{MemorySensitivityLimit: models.MemorySensitivityPublic}}, nil)
	require.NotNil(t, in)
	assert.Equal(t, "public", in.MemorySensitivityLimit)

	mem := &models.Memory{ID: uuid.New(), Scope: "User"}
	in = contextInputs(&chatContext{chat: &models.Chat{MemorySensitivityLimit: models.MemorySensitivityPersonal}, liveMemories: []*models.Memory{mem}, prefetchedMemoryCount: 1}, nil)
	require.NotNil(t, in)
	assert.Equal(t, "personal", in.MemorySensitivityLimit)

	// Unrestricted chats record the default when a manifest is written at all, and stay nil otherwise.
	in = contextInputs(&chatContext{chat: &models.Chat{}, liveMemories: []*models.Memory{mem}, prefetchedMemoryCount: 1}, nil)
	require.NotNil(t, in)
	assert.Equal(t, "sensitive", in.MemorySensitivityLimit)
	assert.Nil(t, contextInputs(&chatContext{chat: &models.Chat{}}, nil))

	raw, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"memory_sensitivity_limit":"sensitive"`)
}

// --- extraction ---------------------------------------------------------------

func TestMemoryExtractionSchema_SensitivityIsPersonalOrSensitiveOnly(t *testing.T) {
	t.Parallel()
	schema := memoryExtractionSchemaMapForClaude()
	items := schema["properties"].(map[string]any)["memories"].(map[string]any)["items"].(map[string]any)
	if ref, ok := items["$ref"].(string); ok { // the generator may hoist the item type into $defs
		name := ref[strings.LastIndex(ref, "/")+1:]
		items = schema["$defs"].(map[string]any)[name].(map[string]any)
	}
	props := items["properties"].(map[string]any)
	sens, ok := props["sensitivity"].(map[string]any)
	require.True(t, ok, "the strict extraction schema carries a sensitivity field")
	require.Equal(t, []any{"personal", "sensitive"}, sens["enum"], "extraction never emits public")
	require.Contains(t, items["required"], "sensitivity", "strict structured output needs every field required")

	require.Contains(t, memoryWritePromptText(), "sensitivity", "the prompt tells the model how to label")
	require.Contains(t, memoryWritePromptText(), "never use \"public\"")
}

func TestExtractedSensitivityNormalizesAndCollapsesToMostRestricted(t *testing.T) {
	t.Parallel()
	got := memoryutil.NormalizeExtractedMemories([]models.ExtractedMemory{
		{Content: "a", Scope: "User", Sensitivity: "sensitive"},
		{Content: "b", Scope: "User"},
		{Content: "c", Scope: "User", Sensitivity: "public"},
		{Content: "d", Scope: "User", Sensitivity: " SENSITIVE "},
		{Content: "e", Scope: "User", Sensitivity: "nonsense"},
	}, 0)
	var levels []models.MemorySensitivity
	for _, m := range got {
		levels = append(levels, m.Sensitivity)
	}
	require.Equal(t, []models.MemorySensitivity{"sensitive", "personal", "personal", "sensitive", "personal"}, levels,
		"absent, public and unknown read as personal; extraction can only raise to sensitive")

	collapsed := memoryutil.CollapseExtractedMemories([]models.ExtractedMemory{
		{Content: "Has asthma", Scope: "User", Sensitivity: "personal"},
		{Content: "has  asthma", Scope: "User", Sensitivity: "sensitive"},
		{Content: "Likes tea", Scope: "User"},
	})
	require.Len(t, collapsed, 2)
	require.Equal(t, models.MemorySensitivitySensitive, collapsed[0].Sensitivity, "duplicates collapse to the most restricted")
	require.Equal(t, models.MemorySensitivityPersonal, collapsed[1].Sensitivity)
}

func TestPlanMemoryCompaction_CarriesNewMemberSensitivityAndChatLimit(t *testing.T) {
	t.Parallel()
	liveID := uuid.New()
	extracted := []memoryutil.CollapsedExtractedMemory{
		{Content: "Takes beta blockers", Scope: "User", Confidence: models.MemoryConfidenceHigh, BatchDuplicateCount: 1, Sensitivity: models.MemorySensitivitySensitive},
		{Content: "Likes tea", Scope: "User", Confidence: models.MemoryConfidenceHigh, BatchDuplicateCount: 1, Sensitivity: models.MemorySensitivityPersonal},
	}
	live := []*models.Memory{{ID: liveID, Content: "Has a heart condition", Scope: "User", Confidence: 0.6, Sensitivity: models.MemorySensitivityPublic}}
	candidates := buildMemoryMergeCandidates(nil, live, extracted)
	require.Len(t, candidates, 3)
	require.Equal(t, models.MemorySensitivity(""), candidates[0].Sensitivity, "stored memories keep their own level in the database")
	require.Equal(t, models.MemorySensitivitySensitive, candidates[1].Sensitivity)

	groups := []models.MemoryMergeGroupProposal{
		{MemberIndices: []int{0, 1}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "Has a heart condition, takes beta blockers", Scope: "User", Confidence: models.MemoryConfidenceHigh},
		{MemberIndices: []int{2}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "Likes tea", Scope: "User", Confidence: models.MemoryConfidenceHigh},
	}
	plan := planMemoryCompaction(groups, candidates)
	require.Len(t, plan.Folds, 2)
	require.Equal(t, models.MemorySensitivitySensitive, plan.Folds[0].NewSensitivity)
	require.Equal(t, models.MemorySensitivityPersonal, plan.Folds[1].NewSensitivity)

	// A fold of stored members only reports no level of its own, so it cannot raise a survivor.
	storedOnly := planMemoryCompaction([]models.MemoryMergeGroupProposal{{MemberIndices: []int{0}, Relation: models.MemoryMergeRelationMerge, CanonicalContent: "x", Scope: "User"}}, candidates[:1])
	if len(storedOnly.Folds) > 0 {
		require.Equal(t, models.MemorySensitivity(""), storedOnly.Folds[0].NewSensitivity)
	}

	// New members are capped by the asking chat's limit; a group without new members passes nothing.
	for _, tc := range []struct {
		limit models.MemorySensitivity
		want  models.MemorySensitivity
	}{
		{"", models.MemorySensitivitySensitive},
		{models.MemorySensitivitySensitive, models.MemorySensitivitySensitive},
		{models.MemorySensitivityPersonal, models.MemorySensitivityPersonal},
		{models.MemorySensitivityPublic, models.MemorySensitivityPublic},
	} {
		require.Equal(t, tc.want, foldNewMemberSensitivity(plan.Folds[0], tc.limit), "limit %q", tc.limit)
		require.Len(t, foldMemberOptions(plan.Folds[0], tc.limit), 1)
	}
	require.Equal(t, models.MemorySensitivityPersonal, foldNewMemberSensitivity(plan.Folds[1], models.MemorySensitivitySensitive))
	require.Equal(t, models.MemorySensitivityPublic, foldNewMemberSensitivity(plan.Folds[1], models.MemorySensitivityPublic), "a public chat's writes stay public")
	require.Nil(t, foldMemberOptions(memoryFoldPlan{}, models.MemorySensitivityPublic))
	require.Equal(t, models.MemorySensitivity(""), foldNewMemberSensitivity(memoryFoldPlan{}, models.MemorySensitivityPublic))

	// Link plans keep each new member's own level.
	linkPlan := planMemoryCompaction([]models.MemoryMergeGroupProposal{{
		MemberIndices: []int{0, 1}, Relation: models.MemoryMergeRelationLink, CanonicalContent: "angles", Scope: "User",
	}}, []memoryMergeCandidate{
		{Content: "one", Scope: "User", IsNew: true, Sensitivity: models.MemorySensitivitySensitive},
		{Content: "two", Scope: "User", IsNew: true, Sensitivity: models.MemorySensitivityPersonal},
	})
	require.Len(t, linkPlan.Links, 1)
	require.Equal(t, models.MemorySensitivitySensitive, linkPlan.Links[0].NewMembers[0].Sensitivity)
	require.Equal(t, models.MemorySensitivityPersonal, linkPlan.Links[0].NewMembers[1].Sensitivity)
}

func TestMemoryExtractionDeveloperMessage_RestrictedChatHasNoScratchpadDelta(t *testing.T) {
	t.Parallel()
	restricted := &chatContext{chat: &models.Chat{MemorySensitivityLimit: models.MemorySensitivityPublic}}
	open := &chatContext{chat: &models.Chat{}}
	require.NotContains(t, memoryExtractionDeveloperMessageFor(restricted), "scratchpad")
	require.Contains(t, memoryExtractionDeveloperMessageFor(open), "scratchpad")
	require.Equal(t, memoryExtractionDeveloperMessage, memoryExtractionDeveloperMessageFor(nil))
}

func TestSubagentScratchpad_RestrictedChatSendsNone(t *testing.T) {
	t.Parallel()
	require.Equal(t, "notes", subagentScratchpad(&models.Chat{}, "notes"))
	require.Equal(t, "notes", subagentScratchpad(&models.Chat{MemorySensitivityLimit: models.MemorySensitivitySensitive}, "notes"))
	require.Empty(t, subagentScratchpad(&models.Chat{MemorySensitivityLimit: models.MemorySensitivityPersonal}, "notes"))
	require.Empty(t, subagentScratchpad(&models.Chat{MemorySensitivityLimit: models.MemorySensitivityPublic}, "notes"))
	// The context built from it then carries no scratchpad segment.
	mc := buildSubagentModelContext("prompt", subagentScratchpad(&models.Chat{MemorySensitivityLimit: models.MemorySensitivityPublic}, "notes"), "task")
	require.Empty(t, segmentText(mc, provider.SegmentKindScratchpad))
}
