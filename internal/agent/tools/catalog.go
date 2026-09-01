package tools

import "github.com/openai/openai-go/v3/responses"

// FunctionToolDefinition describes one provider-agnostic function tool.
// Provider packages project these specs into SDK-specific request types.
type FunctionToolDefinition struct {
	Spec             FunctionToolSpec
	HumanDescription string
	AgentDefault     bool
	MoodOnly         bool
	UserToggleable   bool
}

var functionToolCatalog = []FunctionToolDefinition{
	{Spec: UpdateScratchpadToolSpec, HumanDescription: "Update this personality's working notes, which persist across conversations using the same personality.", AgentDefault: true, UserToggleable: true},
	{Spec: CreateMemoryToolSpec, HumanDescription: "Save useful information to memory for this conversation or across future conversations.", AgentDefault: true, UserToggleable: true},
	{Spec: ListToolSpec, HumanDescription: "Browse available models, personalities, skills, files, conversations, jobs, and MCP servers.", AgentDefault: true, UserToggleable: true},
	{Spec: ListMoodsToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: ChangeMoodToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: RunSubagentToolSpec, HumanDescription: "Run a focused sub-agent with an optional personality, model, or skills and return its result.", AgentDefault: true, UserToggleable: true},
	{Spec: GenerateImageToolSpec, HumanDescription: "Create an image from a written description.", AgentDefault: true, UserToggleable: true},
	{Spec: CreateAgentJobToolSpec, HumanDescription: "Create a one-time or recurring task using natural-language timing. Run it in the current or a new thread, optionally with a different model or skills.", AgentDefault: true, UserToggleable: true},
	{Spec: RecallToolSpec, HumanDescription: "Search or retrieve memories, files, summaries, and conversation history. Often use list first to find an item, then find_context to inspect or retrieve its contents.", AgentDefault: true, UserToggleable: true},
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
