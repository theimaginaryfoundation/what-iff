package provider

import (
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func chatCompletionToolNames(tools []openai.ChatCompletionToolUnionParam) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.OfFunction.Function.Name)
	}
	return out
}

func TestReplaceChatCompletionMCPToolsSwapsOnlyMCPTools(t *testing.T) {
	agentTool := OpenAIChatCompletionFunctionTool("load_mcp_tools", "", nil, nil)
	oldMCP := OpenAIChatCompletionFunctionTool("mcp__abc__old", "", nil, nil)
	newMCP := OpenAIChatCompletionFunctionTool("mcp__abc__new", "", nil, nil)

	a := NewMistralAdapter(nil, openai.ChatCompletionNewParams{}, []openai.ChatCompletionToolUnionParam{agentTool, oldMCP}, nil)
	a.SetMCPTools([]openai.ChatCompletionToolUnionParam{newMCP})
	require.Equal(t, []string{"load_mcp_tools", "mcp__abc__new"}, chatCompletionToolNames(a.params.Tools))

	a.SetMCPTools(nil)
	require.Equal(t, []string{"load_mcp_tools"}, chatCompletionToolNames(a.params.Tools))

	// ForceFinalResponse strips tools; a later sync must not re-add them.
	a.params.Tools = nil
	a.SetMCPTools([]openai.ChatCompletionToolUnionParam{newMCP})
	require.Nil(t, a.params.Tools)
}

func TestClaudeAdapterSetMCPToolsKeepsNonMCPTools(t *testing.T) {
	agentTool := ClaudeFunctionTool("list", "", nil, nil, false)
	a := NewClaudeAdapter(nil, anthropic.MessageNewParams{}, []anthropic.ToolUnionParam{agentTool}, true, nil, nil)
	a.SetMCPTools([]anthropic.ToolUnionParam{ClaudeFunctionTool("mcp__abc__search", "", nil, nil, false)})

	var names []string
	webSearch := false
	for _, t := range a.params.Tools {
		if t.OfWebSearchTool20250305 != nil {
			webSearch = true
			continue
		}
		names = append(names, claudeToolName(t))
	}
	require.True(t, webSearch)
	require.Equal(t, []string{"list", "mcp__abc__search"}, names)
}

func TestOpenAIAdapterSetMCPToolsKeepsNonMCPTools(t *testing.T) {
	fn := func(name string) responses.ToolUnionParam {
		return responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{Name: name}}
	}
	a := NewOpenAIAdapter(nil, responses.ResponseNewParams{Tools: []responses.ToolUnionParam{fn("list"), fn("mcp__abc__old")}})
	a.SetMCPTools([]responses.ToolUnionParam{fn("mcp__abc__new")})
	require.Len(t, a.params.Tools, 2)
	require.Equal(t, "list", a.params.Tools[0].OfFunction.Name)
	require.Equal(t, "mcp__abc__new", a.params.Tools[1].OfFunction.Name)
}
