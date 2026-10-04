package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// getAgentToolsList returns the list of agent tools for the given context.
// disabledTools, if non-nil, removes tools whose names appear in the set.
func getAgentToolsList(disabledTools map[string]bool, includeMoodTools bool) []responses.ToolUnionParam {
	specs := tools.AgentFunctionToolSpecs(includeMoodTools)
	out := make([]tools.FunctionToolSpec, 0, len(specs))
	for _, spec := range specs {
		if disabledTools[spec.Name] {
			continue
		}
		out = append(out, spec)
	}
	return tools.OpenAIFunctionTools(out)
}

// dispatchToolUse routes a provider-agnostic ToolUse to the appropriate tool
// implementation. Both the OpenAI and Claude paths converge here since tool
// inputs are normalized to json.RawMessage before reaching this function.
func (a *Agent) dispatchToolUse(ctx context.Context, chatCtx *chatContext, use provider.ToolUse) (string, []*models.FileAttachment, error) {
	handler, ok := a.toolHandlers(chatCtx)[use.Name]
	if !ok {
		if strings.HasPrefix(use.Name, "mcp__") {
			return a.dispatchMCPToolUse(ctx, chatCtx, use)
		}
		return "", nil, fmt.Errorf("unknown tool: %s", use.Name)
	}
	return handler(ctx, use.Input)
}

type toolHandler func(context.Context, []byte) (string, []*models.FileAttachment, error)

func (a *Agent) toolHandlers(chatCtx *chatContext) map[string]toolHandler {
	handlers := map[string]toolHandler{
		tools.UpdateScratchpadToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.scratchpadTool.UpdateScratchpadTool(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.CreateMemoryToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.memoryTool.CreateMemoryTool(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.ListToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.listTool.List(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.ListMoodsToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.listMoodsTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.ChangeMoodToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.changeMoodTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.RunSubagentToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.runSubagentTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.CreateAgentJobToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.createAgentJobTool(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.LoadMCPToolsToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.loadMCPToolsTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.UnloadMCPToolsToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.unloadMCPToolsTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.GenerateImageToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			return a.generateImageTool(ctx, chatCtx.chat, input)
		},
		tools.WebSearchFunctionToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.webSearchTool(ctx, input)
			return out, nil, err
		},
		tools.FetchPageToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.fetchPageTool(ctx, input)
			return out, nil, err
		},
		tools.ReadFileToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.fileReadTool.ReadFile(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.WriteFileToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.workspaceTool.WriteFile(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.RecallEntityToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.entityTool.RecallEntity(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.RememberEntityToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.entityTool.RememberEntity(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.GrepFilesToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.fileReadTool.GrepFiles(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.RecallToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, memories, attachments, err := a.recallTool.Recall(ctx, chatCtx.chat, input)
			if err == nil && len(memories) > 0 {
				chatCtx.addLoadedMemories(memories)
			}
			return out, attachments, err
		},
	}
	if extraToolHandlersForChat != nil {
		for name, h := range extraToolHandlersForChat(a, chatCtx.chat) {
			if h == nil {
				continue
			}
			handlers[name] = toolHandler(h)
		}
	}
	return handlers
}

func (a *Agent) dispatchMCPToolUse(ctx context.Context, chatCtx *chatContext, use provider.ToolUse) (string, []*models.FileAttachment, error) {
	if a.mcpClient == nil {
		return "", nil, fmt.Errorf("mcp client is not configured")
	}
	if chatCtx == nil || chatCtx.chat == nil {
		return "", nil, fmt.Errorf("mcp tool requires an active chat context")
	}
	servers := chatCtx.mcpServers
	if len(servers) == 0 {
		servers = a.getChatMCPServers(ctx, chatCtx.userID, chatCtx.chat.ID, nil)
		loadedByServer, err := a.ds.ListChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID)
		if err != nil {
			a.logger.Warn("failed to load chat MCP loaded-tool state for dispatch",
				zap.String("chat_id", chatCtx.chat.ID.String()),
				zap.String("user_id", chatCtx.userID.String()),
				zap.Error(err))
			return "", nil, fmt.Errorf("unable to read loaded MCP tool state; retry after loading tools again")
		}
		chatCtx.setMCPServerCache(servers, loadedByServer)
	}
	if !mcpToolIsLoaded(chatCtx, use.Name) {
		return "", nil, fmt.Errorf("mcp tool %q is not loaded for this chat; call %q first", use.Name, tools.LoadMCPToolsToolSpec.Name)
	}
	out, err := a.mcpClient.CallToolByFullNameWithSessionStateDetailed(ctx, servers, use.Name, use.Input, sessionStateFromChatContext(chatCtx))
	if err != nil {
		return "", nil, err
	}
	return out.Output, out.GeneratedAttachments, nil
}

func mcpToolIsLoaded(chatCtx *chatContext, fullToolName string) bool {
	if chatCtx == nil {
		return false
	}
	name := strings.TrimSpace(fullToolName)
	if name == "" {
		return false
	}
	for _, server := range chatCtx.mcpServers {
		if server == nil || server.ID == uuid.Nil {
			continue
		}
		set := chatCtx.loadedMCPTools[server.ID]
		if len(set) == 0 {
			continue
		}
		if _, ok := set[name]; ok {
			return true
		}
	}
	return false
}
