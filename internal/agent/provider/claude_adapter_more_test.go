package provider

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestExtractClaudeToolUses_NilMessage(t *testing.T) {
	t.Parallel()
	require.Empty(t, extractClaudeToolUses(nil))
}

func TestExtractClaudeToolUses_FromContentBlocks(t *testing.T) {
	t.Parallel()
	raw := `{"type":"tool_use","id":"toolu_01","name":"update_scratchpad","input":{"x":1}}`
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &block))

	msg := &anthropic.Message{Content: []anthropic.ContentBlockUnion{block}}
	uses := extractClaudeToolUses(msg)
	require.Len(t, uses, 1)
	require.Equal(t, "toolu_01", uses[0].ID)
	require.Equal(t, "update_scratchpad", uses[0].Name)
	require.JSONEq(t, `{"x":1}`, string(uses[0].Input))
}

func TestAppendClaudeAssistantLoopTurn_ReplaysNativeWebSearchBlocks(t *testing.T) {
	t.Parallel()
	var textBlock anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"text","text":"I'll search and update."}`), &textBlock))

	var serverUse anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"server_tool_use","id":"srv_01","name":"web_search","input":{"query":"weather"}}`), &serverUse))

	var webResult anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"web_search_tool_result",
		"tool_use_id":"srv_01",
		"content":[
			{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"enc_abc","page_age":"1d"}
		]
	}`), &webResult))

	var clientUse anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"tool_use","id":"toolu_01","name":"update_scratchpad","input":{"x":1}}`), &clientUse))

	msg := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{textBlock, serverUse, webResult, clientUse},
	}
	uses := extractClaudeToolUses(msg)
	require.Len(t, uses, 1)

	params := anthropic.MessageNewParams{
		Model: anthropic.Model("claude-sonnet-4-6"),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
	}
	appendClaudeAssistantLoopTurn(&params, msg, uses)

	require.Len(t, params.Messages, 2)
	assistant := params.Messages[1]
	require.Equal(t, anthropic.MessageParamRoleAssistant, assistant.Role)
	require.Len(t, assistant.Content, 4, "expected text + server_tool_use + web_search + client tool_use")

	raw, err := json.Marshal(assistant.Content)
	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, "web_search_tool_result")
	require.Contains(t, body, "server_tool_use")
	require.Contains(t, body, "enc_abc")
	require.Contains(t, body, "toolu_01")
}

func TestAppendClaudeAssistantLoopTurn_EmptyWebSearchUsesTextFallback(t *testing.T) {
	t.Parallel()
	var webResult anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[]}`), &webResult))

	var clientUse anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"tool_use","id":"toolu_01","name":"update_scratchpad","input":{}}`), &clientUse))

	msg := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{webResult, clientUse},
	}
	uses := extractClaudeToolUses(msg)

	params := anthropic.MessageNewParams{
		Model: anthropic.Model("claude-sonnet-4-6"),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
	}
	appendClaudeAssistantLoopTurn(&params, msg, uses)

	require.Len(t, params.Messages, 2, "empty web search is not replayed natively; only assistant tool_use")
	assistant := params.Messages[1]
	raw, err := json.Marshal(assistant.Content)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "web_search_tool_result")
	require.Contains(t, string(raw), "toolu_01")
}

func TestAppendClaudeAssistantLoopTurn_DropsInvalidWebSearchCitations(t *testing.T) {
	t.Parallel()

	var textBlock anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"text",
		"text":"I found a result.",
		"citations":[{"type":"web_search_result_location","url":"","title":"Untitled","cited_text":"result","encrypted_index":"idx"}]
	}`), &textBlock))
	var serverUse anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"server_tool_use","id":"srv_01","name":"web_search","input":{"query":"weather"}}`), &serverUse))
	var webResult anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Weather","url":"https://example.com","encrypted_content":"enc"}]}`), &webResult))
	var clientUse anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"tool_use","id":"toolu_01","name":"update_scratchpad","input":{}}`), &clientUse))

	msg := &anthropic.Message{Content: []anthropic.ContentBlockUnion{textBlock, serverUse, webResult, clientUse}}
	params := anthropic.MessageNewParams{Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))}}
	appendClaudeAssistantLoopTurn(&params, msg, extractClaudeToolUses(msg))

	raw, err := json.Marshal(params.Messages[1].Content)
	require.NoError(t, err)
	require.Contains(t, string(raw), "I found a result.")
	require.NotContains(t, string(raw), `"citations"`)
	require.Contains(t, string(raw), `"encrypted_content":"enc"`)
}

func TestClaudeAdapter_AppendToolResults_UserTurnWithBlocks(t *testing.T) {
	t.Parallel()
	base := anthropic.MessageNewParams{
		Model: anthropic.Model("claude-3-5-sonnet-20241022"),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
	}
	a := NewClaudeAdapter(nil, base, nil, false, nil, nil)
	a.AppendToolResults([]ToolResult{
		{ID: "call_a", Output: `{"ok":true}`, IsErr: false},
		{ID: "call_b", Output: "", IsErr: true},
	})

	require.Len(t, a.params.Messages, 2)
	last := a.params.Messages[1]
	require.GreaterOrEqual(t, len(last.Content), 2)
}

func TestClaudeAdapter_AppendToolResults_InjectsGeneratedImagesOnUserTurn(t *testing.T) {
	t.Parallel()
	base := anthropic.MessageNewParams{
		Model: anthropic.Model("claude-sonnet-4-6"),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
	}
	a := NewClaudeAdapter(nil, base, nil, false, nil, nil)
	a.AppendToolResults([]ToolResult{
		{
			ID:     "call_img",
			Output: `{"success":true}`,
			Images: []UserMessageImage{
				{RawBytes: []byte{0x89, 0x50, 0x4e, 0x47}, MediaType: "image/png"},
			},
		},
	})

	require.Len(t, a.params.Messages, 3, "tool results user turn + image user turn")
	raw, err := json.Marshal(a.params.Messages[2].Content)
	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, GeneratedToolImageCaption)
	require.Contains(t, body, "image")
}

func TestNewClaudeAdapter_WithMCPConfig_UsesBetaMode(t *testing.T) {
	t.Parallel()
	base := anthropic.MessageNewParams{
		Model: anthropic.Model("claude-sonnet-4-6"),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
		MaxTokens: 128,
	}
	mcp := &ClaudeMCPConfig{
		Servers: []anthropic.BetaRequestMCPServerURLDefinitionParam{
			{Name: "mcp-a", URL: "https://mcp.example.com"},
		},
		Toolsets: []anthropic.BetaToolUnionParam{
			anthropic.BetaToolUnionParamOfMCPToolset("mcp-a"),
		},
	}
	a := NewClaudeAdapter(nil, base, nil, true, mcp, nil)
	require.True(t, a.useBetaMCP)
	require.NotEmpty(t, a.betaParams.Betas)
	require.Len(t, a.betaParams.MCPServers, 1)
}

func TestNewClaudeAdapter_IncludeMoodToolsWhenRequested(t *testing.T) {
	t.Parallel()
	base := anthropic.MessageNewParams{
		Model: anthropic.Model("claude-3-5-sonnet-20241022"),
	}
	moodTools := []anthropic.ToolUnionParam{
		ClaudeFunctionTool("list_modes", "List modes", map[string]interface{}{}, []string{}, false),
		ClaudeFunctionTool("change_mode", "Change mode", map[string]interface{}{}, []string{}, false),
	}
	a := NewClaudeAdapter(nil, base, moodTools, false, nil, nil)

	var hasListModes, hasChangeMode bool
	for _, t := range a.params.Tools {
		if t.OfTool == nil {
			continue
		}
		switch t.OfTool.Name {
		case "list_modes":
			hasListModes = true
		case "change_mode":
			hasChangeMode = true
		}
	}
	require.True(t, hasListModes)
	require.True(t, hasChangeMode)
}

func TestBuildClaudeBetaMCPParams_WebSearchToolOmitsInputSchema(t *testing.T) {
	t.Parallel()
	base := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-sonnet-4-6"),
		MaxTokens: 128,
		Tools: []anthropic.ToolUnionParam{
			claudeWebSearchTool,
		},
	}
	mcp := &ClaudeMCPConfig{
		Servers: []anthropic.BetaRequestMCPServerURLDefinitionParam{
			{Name: "mcp-a", URL: "https://mcp.example.com"},
		},
		Toolsets: []anthropic.BetaToolUnionParam{
			anthropic.BetaToolUnionParamOfMCPToolset("mcp-a"),
		},
	}
	out, err := buildClaudeBetaMCPParams(base, mcp)
	require.NoError(t, err)
	var found bool
	for _, tool := range out.Tools {
		if tool.OfWebSearchTool20250305 == nil {
			continue
		}
		found = true
		raw, err := json.Marshal(tool)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "input_schema", "web_search beta tool must not carry input_schema: %s", string(raw))
	}
	require.True(t, found, "expected web search tool in beta tools list")
}

func TestConvertClaudeToolUnionsToBeta_WebSearchOmitsInputSchema(t *testing.T) {
	t.Parallel()
	out, err := convertClaudeToolUnionsToBeta([]anthropic.ToolUnionParam{
		{OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{}},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	raw, err := json.Marshal(out[0])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "input_schema", string(raw))
}
