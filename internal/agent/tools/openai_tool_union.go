package tools

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// OpenAIToolUnionParam builds a Responses API function tool from a FunctionToolSpec
// (JSON Schema object with type/properties/required).
func OpenAIToolUnionParam(spec FunctionToolSpec) responses.ToolUnionParam {
	parameters := openai.FunctionParameters{
		"type": "object",
	}
	if spec.Properties != nil {
		parameters["properties"] = spec.Properties
	} else {
		parameters["properties"] = map[string]any{}
	}
	if len(spec.Required) > 0 {
		parameters["required"] = spec.Required
	}

	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        spec.Name,
			Description: openai.String(spec.Description),
			Parameters:  parameters,
		},
	}
}
