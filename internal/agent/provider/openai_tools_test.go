package provider

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func TestBuildOpenAITools_OrderAndConcatenation(t *testing.T) {
	t.Parallel()

	chatTools := []responses.ToolUnionParam{
		{OfFunction: &responses.FunctionToolParam{Name: "chat_tool"}},
	}
	agentTools := []responses.ToolUnionParam{
		{OfFunction: &responses.FunctionToolParam{Name: "agent_tool"}},
	}
	mcpTools := []responses.ToolUnionParam{
		{OfFunction: &responses.FunctionToolParam{Name: "mcp_tool"}},
	}

	tools := BuildOpenAITools("gpt-5.1", chatTools, agentTools, mcpTools)
	require.Len(t, tools, 3)
	require.Equal(t, "chat_tool", tools[0].OfFunction.Name)
	require.Equal(t, "agent_tool", tools[1].OfFunction.Name)
	require.Equal(t, "mcp_tool", tools[2].OfFunction.Name)
}

func TestBuildOpenAITools_AddsImageToolForGPT4o(t *testing.T) {
	t.Parallel()

	tools := BuildOpenAITools("gpt-4o", nil, nil, nil)
	require.Len(t, tools, 1)
	require.NotNil(t, tools[0].OfImageGeneration)
	require.Equal(t, "image_generation", string(tools[0].OfImageGeneration.Type))
}
