package tools

import (
	"strings"

	"github.com/openai/openai-go/v3/responses"
)

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
	// SandboxPolicy is what a sandboxed chat does with the tool (see SandboxPolicy). Declare it:
	// the zero value is SandboxNever.
	SandboxPolicy SandboxPolicy
	// Loadable tools follow the MCP tools' lifecycle: the model is not sent their definition until
	// it loads them in the chat with load_tools (they are listed by name in the developer context
	// instead), they stay loaded across turns, and they unload with unload_tools or when a
	// checkpoint finds them unused. For heavy tools whose schemas cost tokens on every request but
	// that most turns never call.
	Loadable bool
}

var functionToolCatalog = []FunctionToolDefinition{
	{Spec: UpdateScratchpadToolSpec, HumanDescription: "Update this personality's working notes, which carry over to every thread with this personality.", UserGuide: "Your personality keeps their own notes here: your preferences, their tone and their habits. Changes carry over to every thread with this personality. Ask to have something noted, or for a change in how they behave.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxNever},
	{Spec: CreateMemoryToolSpec, HumanDescription: "Save useful information to memory for this thread or across future threads.", UserGuide: "Ask to remember a fact or preference. A memory can apply to all your threads or just this one, so say if it's only for this thread.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxAllowed},
	{Spec: ListToolSpec, HumanDescription: "Browse available models, personalities, skills, files, threads, jobs, and MCP servers.", UserGuide: "Browses your threads (including archived and imported ones), files, scheduled jobs, personalities, skills, models and this thread's MCP servers. Ask for threads about a topic, then read one with Find Context.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxAllowed},
	{Spec: ListMoodsToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false, SandboxPolicy: SandboxAllowed},
	{Spec: ChangeMoodToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false, SandboxPolicy: SandboxAllowed},
	{Spec: RunSubagentToolSpec, HumanDescription: "Run a focused sub-agent with an optional personality, model, or skills and return its result.", UserGuide: "Hands one task to a helper, optionally with a different personality, model or skills, and brings back the answer. The helper sees only the task, not this thread. Good for a second opinion, or heavy research a smaller, cheaper model can do.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxDefaultOff, Loadable: true},
	{Spec: MoveFilesToolSpec, HumanDescription: "Sort gallery files into folders, or rename them.", UserGuide: "Moves images and other files into gallery folders such as \"charts\", or back out, and renames them. Ask it to tidy recurring files (\"put this week's charts in a charts folder\") or to give meaningless file names useful ones. Only labels change.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxNever},
	{Spec: GenerateImageToolSpec, HumanDescription: "Create an image from a written description.", UserGuide: "Creates up to 4 images at a time. Choose square, landscape or portrait, and low, medium or high quality (higher quality costs more). Ask for a few options or a specific style.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxDefaultOff, Loadable: true},
	{Spec: CreateAgentJobToolSpec, HumanDescription: "Create a one-time or recurring task using natural-language timing. Run it in the current or a new thread, optionally with a different model or skills.", UserGuide: "Schedules a one-time or repeating task in plain language (\"in 2 hours\", \"every weekday at 8am\"). It can post in this thread or its own, with a chosen model or skills. Manage jobs under Config > Jobs.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxNever, Loadable: true},
	{Spec: UpdateAgentJobToolSpec, HumanDescription: "Change an existing scheduled task: its timing, wording, thread, model or skills, or pause and resume it.", UserGuide: "Edits a job that already exists instead of making a new one: \"move my morning check-in to 9\", \"post that reminder in this thread instead\", \"pause the weekly summary\". You can see every change under Config > Jobs.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxNever, Loadable: true},
	{Spec: LoadToolsToolSpec, HumanDescription: "Load built-in or MCP tools into this chat. Loaded tools persist across turns until unloaded.", AgentDefault: true, UserToggleable: false, SandboxPolicy: SandboxAllowed},
	{Spec: UnloadToolsToolSpec, HumanDescription: "Unload previously loaded built-in or MCP tools from this chat.", AgentDefault: true, UserToggleable: false, SandboxPolicy: SandboxAllowed},
	// First-party web search (ADR 0x021). Not user-toggleable here: GetAvailableTools already lists
	// web_search for the toggle, and fetch_page follows it. The per-turn policy hides both when no
	// backend is configured.
	{Spec: WebSearchFunctionToolSpec, AgentDefault: true, SandboxPolicy: SandboxDefaultOff, Loadable: true},
	{Spec: FetchPageToolSpec, AgentDefault: true, SandboxPolicy: SandboxDefaultOff, Loadable: true},
	{Spec: RecallToolSpec, HumanDescription: "Search or read your memories, files, thread summaries and past threads.", UserGuide: "Ask a question about past threads, memories or files and get an answer with sources. It can also read a whole thread or file, look back over a time range (\"last week\"), show a thread's bookmarks, or explain where a memory came from.", AgentDefault: true, UserToggleable: true, SandboxPolicy: SandboxAllowed},
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

// LoadableFunctionToolNames lists the catalog tools that must be loaded before the model is sent
// their definitions (see FunctionToolDefinition.Loadable), with the one-line summary the developer
// context shows for each.
func LoadableFunctionToolNames() map[string]string {
	out := map[string]string{}
	for _, def := range mergedFunctionToolCatalog() {
		if def.Loadable {
			out[def.Spec.Name] = loadableToolSummary(def)
		}
	}
	return out
}

// loadableToolSummary is the tool's human description, or else the first sentence of its model
// description.
func loadableToolSummary(def FunctionToolDefinition) string {
	if d := strings.TrimSpace(def.HumanDescription); d != "" {
		return d
	}
	d := strings.TrimSpace(def.Spec.Description)
	if i := strings.Index(d, ". "); i >= 0 {
		return d[:i+1]
	}
	return d
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
	MoveFilesToolParam   = OpenAIToolUnionParam(MoveFilesToolSpec)
	ListMoodsTool        = OpenAIToolUnionParam(ListMoodsToolSpec)
	ChangeMoodTool       = OpenAIToolUnionParam(ChangeMoodToolSpec)
	RunSubagentTool      = OpenAIToolUnionParam(RunSubagentToolSpec)
	GenerateImageTool    = OpenAIToolUnionParam(GenerateImageToolSpec)
	CreateAgentJobTool   = OpenAIToolUnionParam(CreateAgentJobToolSpec)
	RecallToolParam      = OpenAIToolUnionParam(RecallToolSpec)
)
