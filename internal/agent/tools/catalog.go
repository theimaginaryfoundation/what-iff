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
	{Spec: UpdateScratchpadToolSpec, HumanDescription: "Update the assistant's temporary working notes for the current conversation.", AgentDefault: true, UserToggleable: true},
	{Spec: CreateMemoryToolSpec, HumanDescription: "Save useful information to long-term memory for future conversations.", AgentDefault: true, UserToggleable: true},
	{Spec: ListToolSpec, HumanDescription: "Browse available conversations, files, jobs, personalities, skills, and other resources.", AgentDefault: true, UserToggleable: true},
	{Spec: ListMoodsToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: ChangeMoodToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: RunSubagentToolSpec, HumanDescription: "Delegate a focused task to a sub-agent and return its result.", AgentDefault: true, UserToggleable: true},
	{Spec: GenerateImageToolSpec, HumanDescription: "Create an image from a written description.", AgentDefault: true, UserToggleable: true},
	{Spec: CreateAgentJobToolSpec, HumanDescription: "Create a scheduled or recurring task for the assistant to run later.", AgentDefault: true, UserToggleable: true},
	{Spec: RecallToolSpec, HumanDescription: "Find relevant memories, files, summaries, and conversation history when more context is needed.", AgentDefault: true, UserToggleable: true},
}

func FunctionToolCatalog() []FunctionToolDefinition {
	out := make([]FunctionToolDefinition, len(functionToolCatalog))
	copy(out, functionToolCatalog)
	return out
}

func AgentFunctionToolSpecs(includeMoodTools bool) []FunctionToolSpec {
	specs := make([]FunctionToolSpec, 0, len(functionToolCatalog))
	for _, def := range functionToolCatalog {
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
	specs := make([]FunctionToolSpec, 0, len(functionToolCatalog))
	for _, def := range functionToolCatalog {
		if def.UserToggleable {
			specs = append(specs, def.Spec)
		}
	}
	return specs
}

func ExecutableFunctionToolSpecs() []FunctionToolSpec {
	specs := make([]FunctionToolSpec, 0, len(functionToolCatalog))
	for _, def := range functionToolCatalog {
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
