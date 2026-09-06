package agent

import (
	"context"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// getSubagentMCPTools returns MCP tool params for ritual-only contexts (e.g. run_subagent).
// It loads ritual MCP servers only — no chat-level MCP servers are included.
func (a *Agent) getSubagentMCPTools(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID, _ string) []responses.ToolUnionParam {
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
	specs := a.discoverMCPFunctionToolSpecs(ctx, userID, servers)
	return agenttools.OpenAIFunctionTools(specs)
}

func (a *Agent) getSubagentMCPFunctionToolSpecs(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID) ([]agenttools.FunctionToolSpec, []*models.MCPServer) {
	if len(ritualIDs) == 0 {
		return nil, nil
	}
	servers, err := a.ds.ListRitualMCPServers(ctx, userID, ritualIDs)
	if err != nil {
		a.logger.Warn("failed to load ritual mcp servers for subagent",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		return nil, nil
	}
	return a.discoverMCPFunctionToolSpecs(ctx, userID, servers), servers
}

func (a *Agent) getChatMCPTools(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID, model string) []responses.ToolUnionParam {
	servers := a.getChatMCPServers(ctx, userID, chatID, ritualIDs)
	specs := a.discoverMCPFunctionToolSpecs(ctx, userID, servers)
	_ = model // preserved for call-site compatibility
	return agenttools.OpenAIFunctionTools(specs)
}

func (a *Agent) prepareTurnMCPToolSpecs(ctx context.Context, chatCtx *chatContext, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) []agenttools.FunctionToolSpec {
	servers := a.getChatMCPServers(ctx, userID, chatID, ritualIDs)
	if chatCtx != nil {
		chatCtx.mcpServers = servers
	}
	return a.discoverMCPFunctionToolSpecs(ctx, userID, servers)
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

func (a *Agent) discoverMCPFunctionToolSpecs(ctx context.Context, userID uuid.UUID, servers []*models.MCPServer) []agenttools.FunctionToolSpec {
	if a.mcpClient == nil || len(servers) == 0 {
		return nil
	}
	out, discoverErr := a.mcpClient.DiscoverTools(ctx, userID, servers)
	if discoverErr != nil {
		a.logger.Warn("mcp tool discovery encountered only connector failures",
			zap.String("user_id", userID.String()),
			zap.Int("connector_count", len(servers)),
			zap.Error(discoverErr))
	}
	now := time.Now().UTC()
	specs := make([]agenttools.FunctionToolSpec, 0, len(out.Tools))
	healthy := map[uuid.UUID]bool{}
	countByConnector := map[uuid.UUID]int{}
	for _, t := range out.Tools {
		props := t.Properties
		if props == nil {
			props = map[string]any{}
		}
		specs = append(specs, agenttools.FunctionToolSpec{
			Name:        t.FullName,
			Description: t.Description,
			Properties:  props,
			Required:    t.Required,
		})
		healthy[t.ConnectorID] = true
		countByConnector[t.ConnectorID]++
	}

	for _, s := range servers {
		if s == nil || s.ID == uuid.Nil {
			continue
		}
		if a.ds == nil {
			continue
		}
		if healthy[s.ID] {
			_ = a.ds.UpdateMCPServerRuntimeState(ctx, userID, s.ID, models.MCPServerStatusActive, "", countByConnector[s.ID], &now, &now)
			continue
		}
		if errMsg := strings.TrimSpace(out.Errors[s.ID]); errMsg != "" {
			_ = a.ds.UpdateMCPServerRuntimeState(ctx, userID, s.ID, models.MCPServerStatusInvalid, errMsg, 0, &now, nil)
			continue
		}
		_ = a.ds.UpdateMCPServerRuntimeState(ctx, userID, s.ID, s.Status, s.StatusReason, 0, &now, s.LastHealthyAt)
	}

	return specs
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
