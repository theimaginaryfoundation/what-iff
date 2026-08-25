package provider

import "github.com/openai/openai-go/v3/responses"

const gpt4oModel = "gpt-4o"

// BuildOpenAITools composes the full OpenAI tool list in canonical order:
// chat tools, then agent tools, then MCP tools, plus the 4o image tool override.
func BuildOpenAITools(
	model string,
	chatTools []responses.ToolUnionParam,
	agentTools []responses.ToolUnionParam,
	mcpTools []responses.ToolUnionParam,
) []responses.ToolUnionParam {
	tools := make([]responses.ToolUnionParam, 0, len(chatTools)+len(agentTools)+len(mcpTools)+1)
	tools = append(tools, chatTools...)
	tools = append(tools, agentTools...)
	tools = append(tools, mcpTools...)

	// Historic behavior: only gpt-4o gets native image_generation tool support.
	if model == gpt4oModel {
		tools = append(tools, responses.ToolUnionParam{
			OfImageGeneration: &responses.ToolImageGenerationParam{Type: "image_generation"},
		})
	}
	return tools
}
