package agent

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// restrictedToolSurface classifies EVERY function tool in the catalog for a restricted chat (memory
// sensitivity limit below sensitive): true means it is offered there, false means it is not. The
// test below fails for any catalog tool missing from this table, so a new tool cannot ship without
// someone deciding whether a sandbox that strangers can talk in may use it, and why.
var restrictedToolSurface = map[string]struct {
	offered bool
	why     string
}{
	agenttools.UpdateScratchpadToolSpec.Name:  {false, "writes the personality-wide scratchpad shared with the owner's other chats"},
	agenttools.CreateAgentJobToolSpec.Name:    {false, "schedules a job in a new unrestricted chat and can attach the owner's skills and their MCP servers"},
	agenttools.CreateMemoryToolSpec.Name:      {true, "level capped to the limit, never public, explicit sensitive kept"},
	agenttools.ListToolSpec.Name:              {true, "jobs, skills, personalities and account-wide files are refused; conversations only at or below this chat's limit"},
	agenttools.ListMoodsToolSpec.Name:         {true, "this chat's own personality moods"},
	agenttools.ChangeMoodToolSpec.Name:        {true, "this chat's own mood"},
	agenttools.RunSubagentToolSpec.Name:       {true, "own persona and no skills; the sub-agent keeps the parent's limit"},
	agenttools.GenerateImageToolSpec.Name:     {true, "no account data read"},
	agenttools.LoadMCPToolsToolSpec.Name:      {true, "only connectors attached to this chat"},
	agenttools.UnloadMCPToolsToolSpec.Name:    {true, "only connectors attached to this chat"},
	agenttools.WebSearchFunctionToolSpec.Name: {true, "no account data read (offered only when a backend is configured)"},
	agenttools.FetchPageToolSpec.Name:         {true, "no account data read (offered only when a backend is configured)"},
	agenttools.RecallToolSpec.Name:            {true, "memories at or below the limit; other conversations only at or below this chat's limit"},
}

// conditionalRestrictedTools are classified tools that are only offered when some deployment
// condition holds (a configured service, an existing binding), so the exact-set test below does not
// expect them from a bare agent. Additional builds register their own conditional tools here.
var conditionalRestrictedTools = map[string]struct{}{
	agenttools.WebSearchFunctionToolSpec.Name: {},
	agenttools.FetchPageToolSpec.Name:         {},
}

func sortedNames(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func TestRestrictedChatToolSurface_EveryCatalogToolIsClassified(t *testing.T) {
	catalog := map[string]struct{}{}
	for _, spec := range agenttools.ExecutableFunctionToolSpecs() {
		catalog[spec.Name] = struct{}{}
		_, ok := restrictedToolSurface[spec.Name]
		require.True(t, ok, "tool %q is in the catalog but not classified in restrictedToolSurface: decide whether a restricted chat may use it, add it there, and (if not) to restrictedChatDisabledTools", spec.Name)
	}
	for name := range restrictedToolSurface {
		_, ok := catalog[name]
		require.True(t, ok, "restrictedToolSurface lists %q, which is no longer a catalog tool", name)
	}
	// And the disabled list is exactly the false rows.
	disabled := map[string]struct{}{}
	for _, name := range restrictedChatDisabledTools {
		disabled[name] = struct{}{}
	}
	for name, row := range restrictedToolSurface {
		_, isDisabled := disabled[name]
		require.Equal(t, !row.offered, isDisabled, "tool %q: restrictedToolSurface and restrictedChatDisabledTools disagree", name)
	}
}

// The exact set of tools offered in a restricted chat versus an unrestricted one, computed through
// the same policy the generation paths use.
func TestRestrictedChatToolSurface_ExactOfferedSets(t *testing.T) {
	a := &Agent{logger: zap.NewNop()} // no web search backend: web_search and fetch_page are hidden for everyone

	offered := func(limit models.MemorySensitivity, showMood bool, userDisabled ...string) []string {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true, MemorySensitivityLimit: limit, DisabledTools: userDisabled}
		policy := a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat}, chat.UserID, &models.ChatMessage{})
		policy.showMoodTools = showMood
		return sortedNames(policy.offeredAgentToolNames())
	}

	wantUnrestricted := func(showMood bool) []string {
		set := map[string]struct{}{}
		for name := range restrictedToolSurface {
			set[name] = struct{}{}
		}
		for name := range conditionalRestrictedTools {
			delete(set, name)
		}
		if !showMood {
			delete(set, agenttools.ListMoodsToolSpec.Name)
			delete(set, agenttools.ChangeMoodToolSpec.Name)
		}
		return sortedNames(set)
	}
	wantRestricted := func(showMood bool) []string {
		set := map[string]struct{}{}
		for _, n := range wantUnrestricted(showMood) {
			if restrictedToolSurface[n].offered {
				set[n] = struct{}{}
			}
		}
		return sortedNames(set)
	}

	for _, showMood := range []bool{false, true} {
		require.Equal(t, wantUnrestricted(showMood), offered("", showMood), "unrestricted chat, mood tools %v", showMood)
		require.Equal(t, wantUnrestricted(showMood), offered(models.MemorySensitivitySensitive, showMood))
		for _, limit := range []models.MemorySensitivity{models.MemorySensitivityPersonal, models.MemorySensitivityPublic, "garbage"} {
			require.Equal(t, wantRestricted(showMood), offered(limit, showMood), "restricted chat (limit %q), mood tools %v", limit, showMood)
		}
	}

	// The concrete difference, spelled out: a restricted chat loses exactly these two.
	require.Contains(t, offered("", false), agenttools.CreateAgentJobToolSpec.Name)
	require.NotContains(t, offered(models.MemorySensitivityPublic, false), agenttools.CreateAgentJobToolSpec.Name)
	require.NotContains(t, offered(models.MemorySensitivityPublic, false), agenttools.UpdateScratchpadToolSpec.Name)
	require.NotContains(t, offered(models.MemorySensitivityPublic, false, "list"), "list", "the user's own disabled_tools still apply on top")
}

// The restricted set also holds when the user's tools are off (the early return in the policy) and
// is what dispatch enforces: a restricted chat's model that emits create_agent_job anyway is
// refused before the handler runs.
func TestRestrictedChat_DispatchRefusesCreateAgentJobAndTheHandlerDoesToo(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: false, MemorySensitivityLimit: models.MemorySensitivityPublic}
	chatCtx := &chatContext{chat: chat}
	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chat.UserID, &models.ChatMessage{})
	require.True(t, policy.disabledTools[agenttools.CreateAgentJobToolSpec.Name], "hidden even when tools are off")
	chatCtx.setOfferedTools(policy.offeredAgentToolNames())

	args := []byte(`{"schedule_input":"every day at 9","prompt":"do the thing","skill_ids":["` + uuid.NewString() + `"],"use_current_thread":false}`)
	_, _, err := a.dispatchToolUse(context.Background(), chatCtx, toolUse(agenttools.CreateAgentJobToolSpec.Name, args))
	require.ErrorContains(t, err, "not available in this conversation", "refused at dispatch, as it was never offered")

	// Even if it were dispatched (a future change to the offered set), the handler refuses on its
	// own and touches nothing: the Agent has no datastore, so reaching it would panic.
	out, err := a.createAgentJobTool(context.Background(), chat, args)
	require.NoError(t, err)
	var res createAgentJobToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.False(t, res.Success)
	require.Contains(t, res.Error, "restricted conversation")
	require.Empty(t, res.AgentJobID)
}

// A restricted chat's active mood contributes no ritual ids to the turn's tools, so the MCP
// servers linked to the mood's skills are neither discovered nor offered there.
func TestRestrictedChat_MoodRitualMCPServersAreNotOffered(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	moodRitual := uuid.New()
	mood := &models.Mood{ID: uuid.New(), RitualIDs: []uuid.UUID{moodRitual}}
	policyFor := func(limit models.MemorySensitivity) turnToolPolicy {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true, MemorySensitivityLimit: limit}
		return a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat, activeMood: mood}, chat.UserID, &models.ChatMessage{})
	}
	require.Contains(t, policyFor("").ritualIDs, moodRitual, "an unrestricted chat loads its mood's skill servers")
	require.NotContains(t, policyFor(models.MemorySensitivityPersonal).ritualIDs, moodRitual)
	require.NotContains(t, policyFor(models.MemorySensitivityPublic).ritualIDs, moodRitual)
}
