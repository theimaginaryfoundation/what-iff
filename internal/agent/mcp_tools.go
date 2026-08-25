package agent

import (
	"context"
	"strconv"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// getSubagentMCPTools returns MCP tool params for ritual-only contexts (e.g. run_subagent).
// It loads ritual MCP servers only — no chat-level MCP servers are included.
func (a *Agent) getSubagentMCPTools(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID, model string) []responses.ToolUnionParam {
	if len(ritualIDs) == 0 {
		return nil
	}
	servers, err := a.ds.ListRitualMCPServers(ctx, userID, ritualIDs)
	if err != nil {
		a.logger.Warn("failed to load ritual mcp servers for subagent",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		return nil
	}
	enableDeferredLoading := modelSupportsMCPDeferLoading(model)
	tools := buildMCPToolsFromServers(servers, enableDeferredLoading)
	return appendToolSearchIfNeeded(tools, enableDeferredLoading)
}

// getSubagentClaudeMCPConfig returns a ClaudeMCPConfig for ritual-only contexts.
func (a *Agent) getSubagentClaudeMCPConfig(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID) *provider.ClaudeMCPConfig {
	if len(ritualIDs) == 0 {
		return nil
	}
	servers, err := a.ds.ListRitualMCPServers(ctx, userID, ritualIDs)
	if err != nil {
		a.logger.Warn("failed to load ritual mcp servers for claude subagent",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		return nil
	}
	return buildClaudeMCPConfigFromServers(servers)
}

func (a *Agent) getChatMCPTools(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID, model string) []responses.ToolUnionParam {
	servers := a.getChatMCPServers(ctx, userID, chatID, ritualIDs)
	enableDeferredLoading := modelSupportsMCPDeferLoading(model)
	tools := buildMCPToolsFromServers(servers, enableDeferredLoading)
	return appendToolSearchIfNeeded(tools, enableDeferredLoading)
}

func (a *Agent) getChatClaudeMCPConfig(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) *provider.ClaudeMCPConfig {
	servers := a.getChatMCPServers(ctx, userID, chatID, ritualIDs)
	return buildClaudeMCPConfigFromServers(servers)
}

func claudeFunctionTools(specs []agenttools.FunctionToolSpec) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(specs))
	for _, spec := range specs {
		out = append(out, provider.ClaudeFunctionTool(spec.Name, spec.Description, spec.Properties, spec.Required, agenttools.ClaudeToolStrict(spec)))
	}
	return out
}

func openAIChatCompletionFunctionTools(specs []agenttools.FunctionToolSpec) []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(specs))
	for _, spec := range specs {
		out = append(out, provider.OpenAIChatCompletionFunctionTool(spec.Name, spec.Description, spec.Properties, spec.Required))
	}
	return out
}

func geminiFunctionTools(specs []agenttools.FunctionToolSpec) []openai.ChatCompletionToolUnionParam {
	return openAIChatCompletionFunctionTools(specs)
}

func (a *Agent) getChatMCPServers(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) []*models.MCPServer {
	servers, err := a.ds.ListChatMCPServers(ctx, userID, chatID)
	if err != nil {
		a.logger.Warn("failed to load chat mcp servers for tool registration",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		return nil
	}
	if len(ritualIDs) > 0 {
		ritualServers, err := a.ds.ListRitualMCPServers(ctx, userID, ritualIDs)
		if err != nil {
			a.logger.Warn("failed to load ritual mcp servers for tool registration",
				zap.String("user_id", userID.String()),
				zap.String("chat_id", chatID.String()),
				zap.Error(err))
		} else {
			seen := make(map[uuid.UUID]struct{}, len(servers))
			for _, server := range servers {
				if server == nil {
					continue
				}
				seen[server.ID] = struct{}{}
			}
			for _, server := range ritualServers {
				if server == nil {
					continue
				}
				if _, exists := seen[server.ID]; exists {
					continue
				}
				servers = append(servers, server)
				seen[server.ID] = struct{}{}
			}
		}
	}

	return servers
}

func buildMCPToolsFromServers(servers []*models.MCPServer, enableDeferredLoading bool) []responses.ToolUnionParam {
	tools := make([]responses.ToolUnionParam, 0, len(servers))
	for _, server := range servers {
		if server == nil {
			continue
		}
		if strings.TrimSpace(server.ErrorMessage) != "" {
			// Skip unhealthy integrations so agent calls don't use known-bad credentials.
			continue
		}

		mcpTool := &responses.ToolMcpParam{
			ServerLabel:       server.Name,
			ServerURL:         param.NewOpt(server.ServerURL),
			ServerDescription: param.NewOpt(server.Description),
			Type:              "mcp",
			RequireApproval: responses.ToolMcpRequireApprovalUnionParam{
				OfMcpToolApprovalSetting: param.NewOpt("never"),
			},
		}

		if token := strings.TrimSpace(server.AuthToken); token != "" {
			mcpTool.Headers = map[string]string{
				"Authorization": token,
			}
		}

		if enableDeferredLoading {
			mcpTool.DeferLoading = param.NewOpt(true)
		}

		tools = append(tools, responses.ToolUnionParam{OfMcp: mcpTool})
	}

	return tools
}

func appendToolSearchIfNeeded(tools []responses.ToolUnionParam, enableDeferredLoading bool) []responses.ToolUnionParam {
	// OpenAI requires tool_search to be present whenever deferred loading is enabled.
	if !enableDeferredLoading || len(tools) == 0 {
		return tools
	}

	toolSearch := responses.ToolUnionParam{
		OfToolSearch: &responses.ToolSearchToolParam{
			Execution: responses.ToolSearchToolExecutionServer,
			Type:      "tool_search",
		},
	}
	return append(tools, toolSearch)
}

func buildClaudeMCPConfigFromServers(servers []*models.MCPServer) *provider.ClaudeMCPConfig {
	cfg := &provider.ClaudeMCPConfig{
		Servers:  make([]anthropic.BetaRequestMCPServerURLDefinitionParam, 0, len(servers)),
		Toolsets: make([]anthropic.BetaToolUnionParam, 0, len(servers)),
	}
	for _, server := range servers {
		if server == nil {
			continue
		}
		if strings.TrimSpace(server.ErrorMessage) != "" {
			continue
		}
		serverName := "mcp-" + server.ID.String()
		def := anthropic.BetaRequestMCPServerURLDefinitionParam{
			Name: serverName,
			URL:  server.ServerURL,
		}
		if token := strings.TrimSpace(server.AuthToken); token != "" {
			def.AuthorizationToken = anthropic.String(token)
		}
		cfg.Servers = append(cfg.Servers, def)
		cfg.Toolsets = append(cfg.Toolsets, anthropic.BetaToolUnionParamOfMCPToolset(serverName))
	}
	if len(cfg.Servers) == 0 {
		return nil
	}
	return cfg
}

func modelSupportsMCPDeferLoading(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if !strings.HasPrefix(m, "gpt-") {
		return false
	}

	version := strings.TrimPrefix(m, "gpt-")
	majorPart := version
	if idx := strings.Index(version, "."); idx >= 0 {
		majorPart = version[:idx]
	}
	majorPart = trimNumericPrefix(majorPart)
	major, err := strconv.Atoi(majorPart)
	if err != nil {
		return false
	}
	if major > 5 {
		return true
	}
	if major < 5 {
		return false
	}

	// For major version 5, require minor version >= 4.
	if !strings.Contains(version, ".") {
		return false
	}
	minorPart := strings.SplitN(version, ".", 2)[1]
	minorPart = trimNumericPrefix(minorPart)
	minor, err := strconv.Atoi(minorPart)
	if err != nil {
		return false
	}
	return minor >= 4
}

func trimNumericPrefix(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
