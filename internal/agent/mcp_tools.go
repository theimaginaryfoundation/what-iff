package agent

import (
	"context"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
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
	specs := a.discoverMCPFunctionToolSpecs(ctx, userID, servers, nil)
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
	return a.discoverMCPFunctionToolSpecs(ctx, userID, servers, nil), servers
}

// prepareTurnMCPToolSpecs returns all discoverable MCP tool definitions for this
// conversation turn. Execution eligibility is enforced separately in
// dispatchMCPToolUse via chatCtx.loadedMCPTools, so load_mcp_tools/unload_mcp_tools
// changes are effective on the next agent-loop iteration without waiting for a new turn.
func (a *Agent) prepareTurnMCPToolSpecs(ctx context.Context, chatCtx *chatContext, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) []agenttools.FunctionToolSpec {
	servers := a.getChatMCPServers(ctx, userID, chatID, ritualIDs)
	loadedByServer := map[uuid.UUID][]string{}
	if a.ds != nil {
		loaded, err := a.ds.ListChatMCPLoadedTools(ctx, userID, chatID)
		if err != nil {
			a.logger.Warn("failed to load chat mcp loaded tool state",
				zap.String("user_id", userID.String()),
				zap.String("chat_id", chatID.String()),
				zap.Error(err))
		} else if loaded != nil {
			loadedByServer = loaded
		}
	}
	if chatCtx != nil {
		chatCtx.setMCPServerCache(servers, loadedByServer)
	}
	// Register all discoverable MCP tool definitions for the turn so that
	// load_mcp_tools/unload_mcp_tools changes take effect on the next loop round.
	// Execution gating still happens in dispatchMCPToolUse via loadedMCPTools.
	specs := a.discoverMCPFunctionToolSpecs(ctx, userID, servers, sessionStateFromChatContext(chatCtx))
	return specs
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

func (a *Agent) discoverMCPFunctionToolSpecs(ctx context.Context, userID uuid.UUID, servers []*models.MCPServer, sessions mcpclient.SessionState) []agenttools.FunctionToolSpec {
	if a.mcpClient == nil || len(servers) == 0 {
		return nil
	}
	out, discoverErr := a.mcpClient.DiscoverToolsWithSessionState(ctx, userID, servers, sessions)
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
			_ = a.ds.UpdateMCPServerRuntimeState(ctx, userID, s.ID, discoveryFailureStatus(s, errMsg), errMsg, 0, &now, nil)
			continue
		}
		_ = a.ds.UpdateMCPServerRuntimeState(ctx, userID, s.ID, s.Status, s.StatusReason, 0, &now, s.LastHealthyAt)
	}

	return specs
}

func sessionStateFromChatContext(chatCtx *chatContext) mcpclient.SessionState {
	if chatCtx == nil {
		return nil
	}
	if chatCtx.mcpSessions == nil {
		chatCtx.mcpSessions = make(map[string]string)
	}
	return mcpclient.SessionState(chatCtx.mcpSessions)
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

func filterMCPToolSpecsByLoaded(specs []agenttools.FunctionToolSpec, loadedByServer map[uuid.UUID][]string) []agenttools.FunctionToolSpec {
	// Intentional behavior: MCP tools are opt-in per chat.
	// If no loaded state exists yet, expose zero MCP tools until load_mcp_tools is called.
	if len(specs) == 0 || len(loadedByServer) == 0 {
		return nil
	}
	loadedSet := make(map[string]struct{}, len(specs))
	for _, names := range loadedByServer {
		for _, name := range names {
			n := strings.TrimSpace(name)
			if n == "" {
				continue
			}
			loadedSet[n] = struct{}{}
		}
	}
	if len(loadedSet) == 0 {
		return nil
	}
	out := make([]agenttools.FunctionToolSpec, 0, len(specs))
	for _, spec := range specs {
		if _, ok := loadedSet[spec.Name]; ok {
			out = append(out, spec)
		}
	}
	return out
}

func discoveryFailureStatus(server *models.MCPServer, errMsg string) string {
	current := strings.TrimSpace(server.Status)
	if current == "" {
		current = models.MCPServerStatusActive
	}
	// Discovery reports "connector status ... not eligible" for disabled/invalid
	// connectors; preserve their state instead of escalating to another status.
	if !statusEligibleForDiscovery(current) {
		return current
	}
	if isPermanentDiscoveryError(errMsg) {
		return models.MCPServerStatusInvalid
	}
	return models.MCPServerStatusRefreshError
}

func statusEligibleForDiscovery(status string) bool {
	switch strings.TrimSpace(status) {
	case models.MCPServerStatusActive, models.MCPServerStatusExpiring, models.MCPServerStatusRefreshError:
		return true
	default:
		return false
	}
}

func isPermanentDiscoveryError(errMsg string) bool {
	msg := strings.ToLower(strings.TrimSpace(errMsg))
	switch {
	case strings.Contains(msg, "returned status 400"),
		strings.Contains(msg, "returned status 401"),
		strings.Contains(msg, "returned status 403"),
		strings.Contains(msg, "returned status 404"),
		strings.Contains(msg, "oauth connector is not authenticated"),
		strings.Contains(msg, "oauth access token is expired"),
		strings.Contains(msg, "server_url host could not be resolved"),
		strings.Contains(msg, "invalid mcp"),
		strings.Contains(msg, "invalid oauth"),
		strings.Contains(msg, "connector status"):
		return true
	default:
		return false
	}
}
