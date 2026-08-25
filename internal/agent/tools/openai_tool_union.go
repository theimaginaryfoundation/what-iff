package tools

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// OpenAIToolUnionParam builds a Responses API function tool from a FunctionToolSpec
// (JSON Schema object with type/properties/required).
func OpenAIToolUnionParam(spec FunctionToolSpec) responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        spec.Name,
			Description: openai.String(spec.Description),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": spec.Properties,
				"required":   spec.Required,
			},
		},
	}
}
