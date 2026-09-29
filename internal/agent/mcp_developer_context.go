package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// mcpDeveloperContextMaxToolsPerConnector caps how many tool names one connector contributes to
// the developer context; the full inventory stays available via list(kind="mcp_servers").
const mcpDeveloperContextMaxToolsPerConnector = 40

// mcpLifecycleDeveloperContext tells the model which MCP connectors this chat has, which tools
// each one offers, and which are already loaded. It is empty when the chat has no connectors.
//
// Naming the connectors, their ids and their tools up front matters for models that do not
// call list(kind="mcp_servers") on their own (GLM, Mistral): a bare "call list first" rule left
// them answering that they had no such tools.
func (a *Agent) mcpLifecycleDeveloperContext(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) string {
	if a.ds == nil {
		return ""
	}
	servers := a.getChatMCPServers(ctx, userID, chatID, ritualIDs)
	if len(servers) == 0 {
		return ""
	}
	loadedByServer, err := a.ds.ListChatMCPLoadedTools(ctx, userID, chatID)
	if err != nil {
		a.logger.Warn("failed to load chat mcp loaded tool state for developer context",
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
	}
	toolsByServer := map[uuid.UUID][]string{}
	var discoveryErrors map[uuid.UUID]string
	if a.mcpClient != nil {
		// Discovery is cached per connector, so the turn's tool registration reuses this result.
		// Per-connector failures are in out.Errors; the aggregate error adds nothing here.
		out, _ := a.mcpClient.DiscoverTools(ctx, userID, servers)
		for _, t := range out.Tools {
			toolsByServer[t.ConnectorID] = append(toolsByServer[t.ConnectorID], t.Name)
		}
		discoveryErrors = out.Errors
		if len(out.Errors) > 0 {
			failed := make([]string, 0, len(out.Errors))
			for id, msg := range out.Errors {
				failed = append(failed, id.String()+": "+msg)
			}
			sort.Strings(failed)
			a.logger.Warn("mcp discovery failed for some connectors while building developer context",
				zap.String("chat_id", chatID.String()),
				zap.Strings("connector_errors", failed))
		}
	}
	return formatMCPLifecycleDeveloperContext(servers, toolsByServer, loadedByServer, discoveryErrors)
}

func formatMCPLifecycleDeveloperContext(servers []*models.MCPServer, toolsByServer, loadedByServer map[uuid.UUID][]string, discoveryErrors map[uuid.UUID]string) string {
	var b strings.Builder
	b.WriteString("MCP connectors in this chat. Their tools are NOT callable until loaded: call load_mcp_tools with the connector's mcp_server_id and the tool names you need (or [\"all\"]). ")
	b.WriteString("Loaded tools can be called right away in this same turn, as mcp__<connector>__<tool>, and stay loaded in later turns until you call unload_mcp_tools. ")
	b.WriteString("When the user asks what tools you have or asks for something a connector covers, use these connectors.\n")
	for _, s := range servers {
		if s == nil || s.ID == uuid.Nil {
			continue
		}
		fmt.Fprintf(&b, "- %q mcp_server_id=%s", strings.TrimSpace(s.Name), s.ID.String())
		if status := strings.TrimSpace(s.Status); status != "" && status != models.MCPServerStatusActive {
			fmt.Fprintf(&b, " status=%s", status)
		}
		names := append([]string(nil), toolsByServer[s.ID]...)
		sort.Strings(names)
		if len(names) > 0 {
			shown := names
			if len(shown) > mcpDeveloperContextMaxToolsPerConnector {
				shown = shown[:mcpDeveloperContextMaxToolsPerConnector]
			}
			fmt.Fprintf(&b, " tools: %s", strings.Join(shown, ", "))
			if extra := len(names) - len(shown); extra > 0 {
				fmt.Fprintf(&b, " (+%d more; see list(kind=\"mcp_servers\"))", extra)
			}
		} else if strings.TrimSpace(discoveryErrors[s.ID]) != "" {
			b.WriteString(" tools: (discovery failed; the connector may need re-authentication. Retry with list(kind=\"mcp_servers\"))")
		} else {
			b.WriteString(" tools: (not discovered yet; call list(kind=\"mcp_servers\"))")
		}
		if loaded := loadedByServer[s.ID]; len(loaded) > 0 {
			l := append([]string(nil), loaded...)
			sort.Strings(l)
			fmt.Fprintf(&b, " loaded: %s", strings.Join(l, ", "))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
