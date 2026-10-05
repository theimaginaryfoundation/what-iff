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

// sandboxedToolSurface classifies EVERY function tool in the catalog for a sandboxed chat: true means it is offered there, false means it is not. The
// test below fails for any catalog tool missing from this table, so a new tool cannot ship without
// someone deciding whether a sandbox that strangers can talk in may use it, and why.
var sandboxedToolSurface = map[string]struct {
	offered bool
	why     string
}{
	agenttools.UpdateScratchpadToolSpec.Name:  {false, "writes the personality-wide scratchpad shared with the owner's other chats"},
	agenttools.MoveFilesToolSpec.Name:         {false, "sorts the owner's account-wide gallery images into folders"},
	agenttools.CreateAgentJobToolSpec.Name:    {false, "schedules a job in a new ordinary chat and can attach the owner's skills and their MCP servers"},
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

// conditionalSandboxedTools are classified tools that are only offered when some deployment
// condition holds (a configured service, an existing binding), so the exact-set test below does not
// expect them from a bare agent. Additional builds register their own conditional tools here.
var conditionalSandboxedTools = map[string]struct{}{
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

func TestSandboxedChatToolSurface_EveryCatalogToolIsClassified(t *testing.T) {
	catalog := map[string]struct{}{}
	for _, spec := range agenttools.ExecutableFunctionToolSpecs() {
		catalog[spec.Name] = struct{}{}
		_, ok := sandboxedToolSurface[spec.Name]
		require.True(t, ok, "tool %q is in the catalog but not classified in sandboxedToolSurface: decide whether a sandboxed chat may use it, add it there, and (if not) to sandboxedChatDisabledTools", spec.Name)
	}
	for name := range sandboxedToolSurface {
		_, ok := catalog[name]
		require.True(t, ok, "sandboxedToolSurface lists %q, which is no longer a catalog tool", name)
	}
	// And the disabled list is exactly the false rows.
	disabled := map[string]struct{}{}
	for _, name := range sandboxedChatDisabledTools {
		disabled[name] = struct{}{}
	}
	for name, row := range sandboxedToolSurface {
		_, isDisabled := disabled[name]
		require.Equal(t, !row.offered, isDisabled, "tool %q: sandboxedToolSurface and sandboxedChatDisabledTools disagree", name)
	}
}

// The exact set of tools offered in a sandboxed chat versus an ordinary one, computed through
// the same policy the generation paths use.
func TestSandboxedChatToolSurface_ExactOfferedSets(t *testing.T) {
	a := &Agent{logger: zap.NewNop()} // no web search backend: web_search and fetch_page are hidden for everyone

	offered := func(sandboxed bool, showMood bool, userDisabled ...string) []string {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true, Sandboxed: sandboxed, DisabledTools: userDisabled}
		policy := a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat}, chat.UserID, &models.ChatMessage{})
		policy.showMoodTools = showMood
		return sortedNames(policy.offeredAgentToolNames())
	}

	wantUnsandboxed := func(showMood bool) []string {
		set := map[string]struct{}{}
		for name := range sandboxedToolSurface {
			set[name] = struct{}{}
		}
		for name := range conditionalSandboxedTools {
			delete(set, name)
		}
		if !showMood {
			delete(set, agenttools.ListMoodsToolSpec.Name)
			delete(set, agenttools.ChangeMoodToolSpec.Name)
		}
		return sortedNames(set)
	}
	wantSandboxed := func(showMood bool) []string {
		set := map[string]struct{}{}
		for _, n := range wantUnsandboxed(showMood) {
			if sandboxedToolSurface[n].offered {
				set[n] = struct{}{}
			}
		}
		return sortedNames(set)
	}

	for _, showMood := range []bool{false, true} {
		require.Equal(t, wantUnsandboxed(showMood), offered(false, showMood), "ordinary chat, mood tools %v", showMood)
		require.Equal(t, wantSandboxed(showMood), offered(true, showMood), "sandboxed chat, mood tools %v", showMood)
	}

	// The concrete difference, spelled out: a sandboxed chat loses exactly these three.
	require.Contains(t, offered(false, false), agenttools.CreateAgentJobToolSpec.Name)
	require.NotContains(t, offered(true, false), agenttools.CreateAgentJobToolSpec.Name)
	require.NotContains(t, offered(true, false), agenttools.UpdateScratchpadToolSpec.Name)
	require.NotContains(t, offered(true, false), agenttools.MoveFilesToolSpec.Name)
	require.NotContains(t, offered(true, false, "list"), "list", "the user's own disabled_tools still apply on top")
}

// The sandboxed set also holds when the user's tools are off (the early return in the policy) and
// is what dispatch enforces: a sandboxed chat's model that emits create_agent_job anyway is
// refused before the handler runs.
func TestSandboxedChat_DispatchRefusesCreateAgentJobAndTheHandlerDoesToo(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: false, Sandboxed: true}
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
	require.Contains(t, res.Error, "sandboxed conversation")
	require.Empty(t, res.AgentJobID)
}

// A sandboxed chat's active mood contributes no ritual ids to the turn's tools, so the MCP
// servers linked to the mood's skills are neither discovered nor offered there.
func TestSandboxedChat_MoodRitualMCPServersAreNotOffered(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	moodRitual := uuid.New()
	mood := &models.Mood{ID: uuid.New(), RitualIDs: []uuid.UUID{moodRitual}}
	policyFor := func(sandboxed bool) turnToolPolicy {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true, Sandboxed: sandboxed}
		return a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat, activeMood: mood}, chat.UserID, &models.ChatMessage{})
	}
	require.Contains(t, policyFor(false).ritualIDs, moodRitual, "an ordinary chat loads its mood's skill servers")
	require.NotContains(t, policyFor(true).ritualIDs, moodRitual)
}
