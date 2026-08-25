package tools

import "github.com/openai/openai-go/v3/responses"

// FunctionToolDefinition describes one provider-agnostic function tool.
// Provider packages project these specs into SDK-specific request types.
type FunctionToolDefinition struct {
	Spec           FunctionToolSpec
	AgentDefault   bool
	MoodOnly       bool
	UserToggleable bool
}

var functionToolCatalog = []FunctionToolDefinition{
	{Spec: UpdateScratchpadToolSpec, AgentDefault: true, UserToggleable: true},
	{Spec: CreateMemoryToolSpec, AgentDefault: true, UserToggleable: true},
	{Spec: ListToolSpec, AgentDefault: true, UserToggleable: true},
	{Spec: ListMoodsToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: ChangeMoodToolSpec, AgentDefault: true, MoodOnly: true, UserToggleable: false},
	{Spec: RunSubagentToolSpec, AgentDefault: true, UserToggleable: true},
	{Spec: GenerateImageToolSpec, AgentDefault: true, UserToggleable: true},
	{Spec: CreateAgentJobToolSpec, AgentDefault: true, UserToggleable: true},
	{Spec: RecallToolSpec, AgentDefault: true, UserToggleable: true},
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
