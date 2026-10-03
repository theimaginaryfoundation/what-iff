package tools

import "github.com/openai/openai-go/v3/responses"

// FunctionToolDefinition describes one provider-agnostic function tool.
// Provider packages project these specs into SDK-specific request types.
type FunctionToolDefinition struct {
	Spec             FunctionToolSpec
	HumanDescription string
	// UserGuide is a short user-facing overview of what the tool can do and which options it
	// takes, shown as the tool's tooltip in the chat Tools tab. It tells people what they can
	// ask for; keep it in step with the spec when parameters change.
	UserGuide      string
	AgentDefault   bool
	MoodOnly       bool
	UserToggleable bool
}

var functionToolCatalog = []FunctionToolDefinition{
	{Spec: UpdateScratchpadToolSpec, HumanDescription: "Update this personality's working notes, which carry over to every thread with this personality.", UserGuide: "Your personality keeps their own notes here: your preferences, their tone and their habits. Changes carry over to every thread with this personality. Ask to have something noted, or for a change in how they behave.", AgentDefault: true, UserToggleable: true},
	{Spec: CreateMemoryToolSpec, HumanDescription: "Save useful information to memory for this thread or across future threads.", UserGuide: "Ask to remember a fact or preference. A memory can apply to all your threads or just this one, so say if it's only for this thread.", AgentDefault: true, UserToggleable: true},
	{Spec: ListToolSpec, HumanDescription: "Browse available models, personalities, skills, files, threads, jobs, and MCP servers.", UserGuide: "Browses your threads (including archived and imported ones), files, scheduled jobs, personalities, skills, models and this thread's MCP servers. Ask for threads about a topic, then read one with Find Context.", AgentDefault: true, UserToggleable: true},
	{Spec: ListMoodsToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: ChangeMoodToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: RunSubagentToolSpec, HumanDescription: "Run a focused sub-agent with an optional personality, model, or skills and return its result.", UserGuide: "Hands one task to a helper, optionally with a different personality, model or skills, and brings back the answer. The helper sees only the task, not this thread. Good for a second opinion, or heavy research a smaller, cheaper model can do.", AgentDefault: true, UserToggleable: true},
	{Spec: GenerateImageToolSpec, HumanDescription: "Create an image from a written description.", UserGuide: "Creates up to 4 images at a time. Choose square, landscape or portrait, and low, medium or high quality (higher quality costs more). Ask for a few options or a specific style.", AgentDefault: true, UserToggleable: true},
	{Spec: CreateAgentJobToolSpec, HumanDescription: "Create a one-time or recurring task using natural-language timing. Run it in the current or a new thread, optionally with a different model or skills.", UserGuide: "Schedules a one-time or repeating task in plain language (\"in 2 hours\", \"every weekday at 8am\"). It can post in this thread or its own, with a chosen model or skills. Manage jobs under Config > Jobs.", AgentDefault: true, UserToggleable: true},
	{Spec: LoadMCPToolsToolSpec, HumanDescription: "Load MCP tools from one connector into this chat. Loaded MCP tools persist across turns until unloaded.", AgentDefault: true, UserToggleable: false},
	{Spec: UnloadMCPToolsToolSpec, HumanDescription: "Unload previously loaded MCP tools from this chat, per connector or globally.", AgentDefault: true, UserToggleable: false},
	// First-party web search (ADR 0x021). Not user-toggleable here: GetAvailableTools already lists
	// web_search for the toggle, and fetch_page follows it. The per-turn policy hides both when no
	// backend is configured.
	{Spec: WebSearchFunctionToolSpec, AgentDefault: true},
	{Spec: FetchPageToolSpec, AgentDefault: true},
	{Spec: ReadFileToolSpec, HumanDescription: "Read exact line ranges of an uploaded text file, or see its outline.", UserGuide: "Reads the exact text of an uploaded text file (code, logs, transcripts, CSV, JSON, notes) a section at a time, or shows its outline first. Ask for a specific part of a long file, by line or by heading.", AgentDefault: true, UserToggleable: true},
	{Spec: GrepFilesToolSpec, HumanDescription: "Find an exact phrase or pattern in uploaded text files.", UserGuide: "Finds every line that mentions a word, phrase or pattern in this thread's files and the personality's documents, with line numbers. Ask where something appears, or search specific files by name.", AgentDefault: true, UserToggleable: true},
	{Spec: WriteFileToolSpec, HumanDescription: "Keep notes in files that last beyond this thread, and update them over time.", UserGuide: "Lets your personality keep files: a notebook shared by all its threads (agent/) and working files for this thread (chat/). It can write, add to, edit or delete them, and never overwrites a newer version by mistake.", AgentDefault: true, UserToggleable: true},
	{Spec: RecallEntityToolSpec, HumanDescription: "Look up what your personality has noted about a person, place or project.", UserGuide: "Your personality keeps short notes (cards) about the people, places and projects that come up, and sees them whenever you mention one by name. This looks one up by name or nickname.", AgentDefault: true, UserToggleable: true},
	{Spec: RememberEntityToolSpec, HumanDescription: "Keep a short note about a person, place or project that keeps coming up.", UserGuide: "Lets your personality keep and update short notes about the people, places and projects in your life, with nicknames, so it remembers who \"John\" is whenever he's mentioned. Ask it to note, correct or forget someone.", AgentDefault: true, UserToggleable: true},
	{Spec: RecallToolSpec, HumanDescription: "Search or read your memories, files, thread summaries and past threads.", UserGuide: "Ask a question about past threads, memories or files and get an answer with sources. It can also read a whole thread or file, look back over a time range (\"last week\"), show a thread's bookmarks, or explain where a memory came from.", AgentDefault: true, UserToggleable: true},
}

func FunctionToolCatalog() []FunctionToolDefinition {
	catalog := mergedFunctionToolCatalog()
	out := make([]FunctionToolDefinition, len(catalog))
	copy(out, catalog)
	return out
}

func AgentFunctionToolSpecs(includeMoodTools bool) []FunctionToolSpec {
	catalog := mergedFunctionToolCatalog()
	specs := make([]FunctionToolSpec, 0, len(catalog))
	for _, def := range catalog {
		if !def.AgentDefault {
			continue
		}
		if def.MoodOnly && !includeMoodTools {
			continue
		}
		specs = append(specs, def.Spec)
	}
	return specs
}

func UserToggleableFunctionToolSpecs() []FunctionToolSpec {
	catalog := mergedFunctionToolCatalog()
	specs := make([]FunctionToolSpec, 0, len(catalog))
	for _, def := range catalog {
		if def.UserToggleable {
			specs = append(specs, def.Spec)
		}
	}
	return specs
}

func ExecutableFunctionToolSpecs() []FunctionToolSpec {
	catalog := mergedFunctionToolCatalog()
	specs := make([]FunctionToolSpec, 0, len(catalog))
	for _, def := range catalog {
		specs = append(specs, def.Spec)
	}
	return specs
}

func OpenAIFunctionTools(specs []FunctionToolSpec) []responses.ToolUnionParam {
	out := make([]responses.ToolUnionParam, 0, len(specs))
	for _, spec := range specs {
		out = append(out, OpenAIToolUnionParam(spec))
	}
	return out
}

var (
	UpdateScratchpadTool = OpenAIToolUnionParam(UpdateScratchpadToolSpec)
	CreateMemoryTool     = OpenAIToolUnionParam(CreateMemoryToolSpec)
	ListToolParam        = OpenAIToolUnionParam(ListToolSpec)
	ListMoodsTool        = OpenAIToolUnionParam(ListMoodsToolSpec)
	ChangeMoodTool       = OpenAIToolUnionParam(ChangeMoodToolSpec)
	RunSubagentTool      = OpenAIToolUnionParam(RunSubagentToolSpec)
	GenerateImageTool    = OpenAIToolUnionParam(GenerateImageToolSpec)
	CreateAgentJobTool   = OpenAIToolUnionParam(CreateAgentJobToolSpec)
	RecallToolParam      = OpenAIToolUnionParam(RecallToolSpec)
)
