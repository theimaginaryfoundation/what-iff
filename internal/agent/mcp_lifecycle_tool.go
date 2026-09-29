package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type mcpToolLifecycleArgs struct {
	MCPServerID string   `json:"mcp_server_id,omitempty"`
	Tools       []string `json:"tools"`
}

// UnmarshalJSON accepts the argument shapes models actually send, not only the declared schema:
// tools as a JSON array, a JSON-encoded array string, a comma-separated string or a single name,
// plus a few common key aliases (server_id, connector_id, tool_names).
func (args *mcpToolLifecycleArgs) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, key := range []string{"mcp_server_id", "server_id", "connector_id", "mcp_server", "connector"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		var id string
		if err := json.Unmarshal(v, &id); err != nil {
			return fmt.Errorf("%s must be a string", key)
		}
		if strings.TrimSpace(id) != "" {
			args.MCPServerID = id
			break
		}
	}
	for _, key := range []string{"tools", "tool_names", "tool"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		names, err := decodeLenientStringList(v)
		if err != nil {
			return fmt.Errorf("%s must be an array of tool names", key)
		}
		if len(names) > 0 {
			args.Tools = names
			break
		}
	}
	return nil
}

func decodeLenientStringList(v json.RawMessage) ([]string, error) {
	var list []string
	if err := json.Unmarshal(v, &list); err == nil {
		return list, nil
	}
	var single string
	if err := json.Unmarshal(v, &single); err != nil {
		return nil, err
	}
	single = strings.TrimSpace(single)
	if strings.HasPrefix(single, "[") {
		if err := json.Unmarshal([]byte(single), &list); err == nil {
			return list, nil
		}
	}
	return strings.Split(single, ","), nil
}

type mcpToolLifecycleResult struct {
	ServerID      string   `json:"mcp_server_id,omitempty"`
	Loaded        []string `json:"loaded,omitempty"`
	AlreadyLoaded []string `json:"already_loaded,omitempty"`
	Unloaded      []string `json:"unloaded,omitempty"`
	NotLoaded     []string `json:"not_loaded,omitempty"`
	Remaining     []string `json:"remaining_loaded,omitempty"`
	Note          string   `json:"note,omitempty"`
	Error         string   `json:"error,omitempty"`
}

func (a *Agent) loadMCPToolsTool(ctx context.Context, chatCtx *chatContext, input []byte) (string, error) {
	if chatCtx == nil || chatCtx.chat == nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: "load_mcp_tools requires an active chat context"})
	}
	var args mcpToolLifecycleArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: fmt.Sprintf("invalid arguments: %v", err)})
	}
	server, err := a.resolveChatMCPServer(ctx, chatCtx, args.MCPServerID)
	if err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: err.Error()})
	}
	serverID := server.ID
	requested := normalizeRequestedTools(args.Tools)
	if len(requested.names) == 0 && !requested.all {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: "tools is required and must include at least one tool name"})
	}
	discovery, err := a.mcpClient.DiscoverTools(ctx, chatCtx.userID, []*models.MCPServer{server})
	if err != nil && len(discovery.Tools) == 0 {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
			ServerID: serverID.String(),
			Error:    fmt.Sprintf("MCP discovery failed for connector %q: %v", server.Name, err),
		})
	}
	catalog := buildConnectorToolCatalog(discovery.Tools, server.ID)
	if len(catalog.byFullName) == 0 {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
			ServerID: serverID.String(),
			Error:    fmt.Sprintf("connector %q has no discoverable tools", server.Name),
		})
	}

	targetNames, unknown := resolveRequestedToolNames(catalog, requested)
	if requested.all {
		targetNames = catalog.allFullNames
	}
	if len(targetNames) == 0 {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
			ServerID: serverID.String(),
			Error:    "none of the requested tools match discoverable MCP tools for this connector",
		})
	}

	loadedByServer, err := a.ds.ListChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID)
	if err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: err.Error()})
	}
	loadedSet := sliceToSet(loadedByServer[server.ID])
	var loaded []string
	var alreadyLoaded []string
	for _, fullName := range targetNames {
		if _, ok := loadedSet[fullName]; ok {
			alreadyLoaded = append(alreadyLoaded, fullName)
			continue
		}
		loadedSet[fullName] = struct{}{}
		loaded = append(loaded, fullName)
	}
	next := setToSortedSlice(loadedSet)
	if err := a.ds.SetChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID, server.ID, next); err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: err.Error()})
	}

	// Keep the loaded connector in the turn cache so same-turn dispatch and tool sync can find it.
	servers := chatCtx.mcpServers
	if !slices.ContainsFunc(servers, func(s *models.MCPServer) bool { return s != nil && s.ID == server.ID }) {
		servers = append(slices.Clone(servers), server)
	}
	chatCtx.setMCPServerCache(servers, loadedByServerWithUpdate(loadedByServer, server.ID, next))
	if len(loaded) > 0 {
		chatCtx.mcpToolsChanged = true
	}
	res := mcpToolLifecycleResult{
		ServerID:      server.ID.String(),
		Loaded:        loaded,
		AlreadyLoaded: alreadyLoaded,
	}
	if len(unknown) > 0 {
		res.Note = fmt.Sprintf("Ignored unknown tool names: %s", strings.Join(unknown, ", "))
	}
	return marshalMCPToolLifecycleResult(res)
}

func (a *Agent) unloadMCPToolsTool(ctx context.Context, chatCtx *chatContext, input []byte) (string, error) {
	if chatCtx == nil || chatCtx.chat == nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: "unload_mcp_tools requires an active chat context"})
	}
	var args mcpToolLifecycleArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: fmt.Sprintf("invalid arguments: %v", err)})
	}
	requested := normalizeRequestedTools(args.Tools)
	if len(requested.names) == 0 && !requested.all {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: "tools is required and must include at least one tool name"})
	}

	if requested.all && strings.TrimSpace(args.MCPServerID) == "" {
		if err := a.ds.ClearAllChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID); err != nil {
			return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: err.Error()})
		}
		chatCtx.setMCPServerCache(chatCtx.mcpServers, map[uuid.UUID][]string{})
		chatCtx.mcpToolsChanged = true
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Note: "Unloaded all MCP tools for this chat."})
	}

	// A bare UUID is taken as-is so tools of a connector detached since loading can still be unloaded.
	serverID, err := uuid.Parse(strings.TrimSpace(args.MCPServerID))
	if err != nil {
		server, resolveErr := a.resolveChatMCPServer(ctx, chatCtx, args.MCPServerID)
		if resolveErr != nil {
			return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{Error: resolveErr.Error()})
		}
		serverID = server.ID
	}
	loadedByServer, err := a.ds.ListChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID)
	if err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: err.Error()})
	}
	current := sliceToSet(loadedByServer[serverID])
	if len(current) == 0 {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
			ServerID: serverID.String(),
			Note:     "No loaded MCP tools for this connector.",
		})
	}

	if requested.all {
		if err := a.ds.ClearChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID, serverID); err != nil {
			return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: err.Error()})
		}
		loadedByServer = loadedByServerWithUpdate(loadedByServer, serverID, nil)
		chatCtx.setMCPServerCache(chatCtx.mcpServers, loadedByServer)
		chatCtx.mcpToolsChanged = true
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
			ServerID:  serverID.String(),
			Unloaded:  setToSortedSlice(current),
			Remaining: []string{},
		})
	}

	var unloaded []string
	var notLoaded []string
	for _, name := range requested.names {
		canonical := strings.TrimSpace(name)
		if canonical == "" {
			continue
		}
		if _, ok := current[canonical]; ok {
			delete(current, canonical)
			unloaded = append(unloaded, canonical)
		} else {
			notLoaded = append(notLoaded, canonical)
		}
	}
	next := setToSortedSlice(current)
	if len(next) == 0 {
		if err := a.ds.ClearChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID, serverID); err != nil {
			return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: err.Error()})
		}
	} else if err := a.ds.SetChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID, serverID, next); err != nil {
		return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{ServerID: serverID.String(), Error: err.Error()})
	}
	loadedByServer = loadedByServerWithUpdate(loadedByServer, serverID, next)
	chatCtx.setMCPServerCache(chatCtx.mcpServers, loadedByServer)
	if len(unloaded) > 0 {
		chatCtx.mcpToolsChanged = true
	}

	return marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
		ServerID:  serverID.String(),
		Unloaded:  unloaded,
		NotLoaded: notLoaded,
		Remaining: next,
	})
}

// resolveChatMCPServer finds the connector a lifecycle call targets. Models do not always pass
// the UUID: some pass the connector name or its mcp__<prefix>__ key, and some omit it when the
// chat has a single connector. Candidates include ritual-attached connectors cached for this
// turn, not only connectors attached to the chat itself. Errors name the valid connectors so
// the model can retry with a correct id.
func (a *Agent) resolveChatMCPServer(ctx context.Context, chatCtx *chatContext, raw string) (*models.MCPServer, error) {
	servers, err := a.lifecycleCandidateServers(ctx, chatCtx)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no MCP connectors are connected to this chat")
	}
	want := strings.TrimSpace(raw)
	if want == "" {
		if len(servers) == 1 {
			return servers[0], nil
		}
		return nil, fmt.Errorf("mcp_server_id is required; connectors in this chat: %s", describeMCPServers(servers))
	}
	if id, err := uuid.Parse(want); err == nil {
		for _, s := range servers {
			if s.ID == id {
				return s, nil
			}
		}
		return nil, fmt.Errorf("mcp connector %s is not connected to this chat; connectors in this chat: %s", id.String(), describeMCPServers(servers))
	}
	key := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(want, "mcp__"), "__"))
	var matches []*models.MCPServer
	for _, s := range servers {
		compactID := strings.ReplaceAll(s.ID.String(), "-", "")
		if strings.EqualFold(strings.TrimSpace(s.Name), want) || (len(key) >= 8 && strings.HasPrefix(compactID, key)) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, fmt.Errorf("invalid mcp_server_id %q; pass one of: %s", raw, describeMCPServers(servers))
}

// lifecycleCandidateServers merges the chat's own connectors with this turn's cached connectors
// (which include ritual/mood-attached ones).
func (a *Agent) lifecycleCandidateServers(ctx context.Context, chatCtx *chatContext) ([]*models.MCPServer, error) {
	chatServers, err := a.ds.ListChatMCPServers(ctx, chatCtx.userID, chatCtx.chat.ID)
	if err != nil {
		return nil, err
	}
	all := append(append([]*models.MCPServer{}, chatServers...), chatCtx.mcpServers...)
	out := make([]*models.MCPServer, 0, len(all))
	seen := map[uuid.UUID]struct{}{}
	for _, s := range all {
		if s == nil || s.ID == uuid.Nil {
			continue
		}
		if _, dup := seen[s.ID]; dup {
			continue
		}
		seen[s.ID] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func describeMCPServers(servers []*models.MCPServer) string {
	parts := make([]string, 0, len(servers))
	for _, s := range servers {
		parts = append(parts, fmt.Sprintf("%q (mcp_server_id=%s)", strings.TrimSpace(s.Name), s.ID.String()))
	}
	return strings.Join(parts, ", ")
}

type requestedTools struct {
	all   bool
	names []string
}

func normalizeRequestedTools(raw []string) requestedTools {
	out := requestedTools{names: make([]string, 0, len(raw))}
	seen := map[string]struct{}{}
	for _, name := range raw {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		if n == "*" || strings.EqualFold(n, "all") {
			out.all = true
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out.names = append(out.names, n)
	}
	return out
}

type connectorToolCatalog struct {
	byFullName   map[string]mcpclient.ConnectorTool
	byName       map[string]string
	allFullNames []string
}

func buildConnectorToolCatalog(tools []mcpclient.ConnectorTool, connectorID uuid.UUID) connectorToolCatalog {
	catalog := connectorToolCatalog{
		byFullName:   make(map[string]mcpclient.ConnectorTool, len(tools)),
		byName:       make(map[string]string, len(tools)),
		allFullNames: make([]string, 0, len(tools)),
	}
	for _, tool := range tools {
		if tool.ConnectorID != connectorID {
			continue
		}
		full := strings.TrimSpace(tool.FullName)
		if full == "" {
			continue
		}
		catalog.byFullName[full] = tool
		localKey := strings.ToLower(strings.TrimSpace(tool.Name))
		if localKey != "" {
			catalog.byName[localKey] = full
		}
		catalog.allFullNames = append(catalog.allFullNames, full)
	}
	sort.Strings(catalog.allFullNames)
	return catalog
}

func resolveRequestedToolNames(catalog connectorToolCatalog, requested requestedTools) ([]string, []string) {
	if requested.all {
		return slices.Clone(catalog.allFullNames), nil
	}
	out := make([]string, 0, len(requested.names))
	unknown := make([]string, 0)
	seen := map[string]struct{}{}
	for _, req := range requested.names {
		r := strings.TrimSpace(req)
		if r == "" {
			continue
		}
		if _, ok := catalog.byFullName[r]; ok {
			if _, dup := seen[r]; !dup {
				seen[r] = struct{}{}
				out = append(out, r)
			}
			continue
		}
		if full, ok := catalog.byName[strings.ToLower(r)]; ok {
			if _, dup := seen[full]; !dup {
				seen[full] = struct{}{}
				out = append(out, full)
			}
			continue
		}
		unknown = append(unknown, req)
	}
	sort.Strings(out)
	sort.Strings(unknown)
	return out, unknown
}

func sliceToSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		n := strings.TrimSpace(item)
		if n == "" {
			continue
		}
		out[n] = struct{}{}
	}
	return out
}

func setToSortedSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func loadedByServerWithUpdate(src map[uuid.UUID][]string, serverID uuid.UUID, names []string) map[uuid.UUID][]string {
	dst := make(map[uuid.UUID][]string, len(src))
	for id, tools := range src {
		dst[id] = slices.Clone(tools)
	}
	if len(names) == 0 {
		delete(dst, serverID)
	} else {
		dst[serverID] = slices.Clone(names)
	}
	return dst
}

func marshalMCPToolLifecycleResult(res mcpToolLifecycleResult) (string, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
