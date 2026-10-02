package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func (t *ListTool) listModels(ctx context.Context) (string, error) {
	modelsList, err := t.store.ListModels(ctx)
	if err != nil {
		return t.fail(listKindModels, fmt.Sprintf("failed to list models: %v", err))
	}
	items := make([]listItem, 0, len(modelsList))
	for _, m := range modelsList {
		if m == nil {
			continue
		}
		name := m.Name
		if m.DisplayName != "" {
			name = m.DisplayName
		}
		items = append(items, listItem{
			ID:          m.ID.String(),
			Name:        name,
			Provider:    m.Provider,
			ToolSupport: boolPtr(m.ToolSupport),
		})
	}
	return t.ok(listKindModels, items, "")
}

func (t *ListTool) listPersonalities(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	limit := clampLimit(a.Limit, listDiscoveryLimit)
	pageNum := clampPage(a.Page)
	page, err := t.store.ListPersonalities(ctx, chat.UserID, pageNum, limit, models.PersonalityFilters{})
	if err != nil {
		return t.fail(listKindPersonalities, fmt.Sprintf("failed to list personalities: %v", err))
	}
	items := make([]listItem, 0, len(page.Results))
	for _, r := range page.Results {
		p, ok := r.(*models.Personality)
		if !ok || p == nil {
			continue
		}
		items = append(items, listItem{
			ID:        p.ID.String(),
			Name:      p.Name,
			UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	return t.okPaged(listKindPersonalities, items, listTruncationNote("personalities", meta, len(items)), meta)
}

func (t *ListTool) listSkills(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	limit := clampLimit(a.Limit, listDiscoveryLimit)
	pageNum := clampPage(a.Page)
	page, err := t.store.ListRituals(ctx, chat.UserID, pageNum, limit, models.RitualFilters{})
	if err != nil {
		return t.fail(listKindSkills, fmt.Sprintf("failed to list skills: %v", err))
	}
	items := make([]listItem, 0, len(page.Results))
	for _, r := range page.Results {
		ritual, ok := r.(*models.Ritual)
		if !ok || ritual == nil {
			continue
		}
		items = append(items, listItem{
			ID:          ritual.ID.String(),
			Name:        ritual.Name,
			Description: ritual.Description,
		})
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	return t.okPaged(listKindSkills, items, listTruncationNote("skills", meta, len(items)), meta)
}

func (t *ListTool) listFiles(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	limit := clampLimit(a.Limit, listDefaultLimit)
	pageNum := clampPage(a.Page)
	filters := models.FileAttachmentFilters{}
	if ft, ok := validateNonEmptyString(a.FileType); ok {
		filters.FileType = &ft
	}
	if f, ok := validateNonEmptyString(a.Filter); ok {
		filters.Name = &f
	}

	switch normalizeFileScope(a.Scope) {
	case listFileScopePersonality:
		if chat.PersonalityID == uuid.Nil {
			return t.ok(listKindFiles, nil, "No active personality in this chat, so no personality-scoped files.")
		}
		pid := chat.PersonalityID
		filters.PersonalityID = &pid
		filters.DocsOnly = boolPtr(true) // user-uploaded docs, excluding expression images
	case listFileScopeConversation:
		filters.GlobalOnly = boolPtr(true) // files uploaded in conversations, not attached to a personality
	}

	page, err := t.store.ListFileAttachments(ctx, chat.UserID, pageNum, limit, filters)
	if err != nil {
		return t.fail(listKindFiles, fmt.Sprintf("failed to list files: %v", err))
	}
	items := make([]listItem, 0, len(page.Results))
	for _, r := range page.Results {
		fa, ok := r.(*models.FileAttachment)
		if !ok || fa == nil {
			continue
		}
		items = append(items, listItem{
			ID:       fa.ID.String(),
			Name:     fa.Name,
			FileType: fa.FileType,
		})
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	return t.okPaged(listKindFiles, items, listTruncationNote("files", meta, len(items)), meta)
}

// listConversations deliberately spans active and archived threads so the agent can
// discover imported history and read it through find_context without rehydration.
func (t *ListTool) listConversations(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	limit := clampLimit(a.Limit, listDefaultLimit)
	pageNum := clampPage(a.Page)
	filters := models.ChatFilters{
		HasMessages:     boolPtr(true), // skip empty shells (created but never messaged)
		IncludeArchived: true,
	}
	// Discovery includes imports too: archived threads can be read safely via
	// find_context without triggering their costly rehydration workflow.
	if f, ok := validateNonEmptyString(a.Filter); ok {
		filters.Query = &f
	}
	page, err := t.store.ListChats(ctx, chat.UserID, pageNum, limit, filters)
	if err != nil {
		return t.fail(listKindConversations, fmt.Sprintf("failed to list conversations: %v", err))
	}
	items := make([]listItem, 0, len(page.Results))
	for _, r := range page.Results {
		c, ok := r.(*models.Chat)
		if !ok || c == nil {
			continue
		}
		items = append(items, listItem{
			ID:        c.ID.String(),
			Name:      c.Name,
			UpdatedAt: rfc3339Ptr(c.LastMessageTime),
			Archived:  c.Archived,
		})
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	return t.okPaged(listKindConversations, items, listTruncationNote("conversations", meta, len(items)), meta)
}

func (t *ListTool) listJobs(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	limit := clampLimit(a.Limit, listDefaultLimit)
	pageNum := clampPage(a.Page)
	filters := models.AgentJobFilters{}
	if !a.IncludeCompleted {
		active := models.AgentJobStatusActive
		filters.Status = &active
	}
	if f, ok := validateNonEmptyString(a.Filter); ok {
		filters.Query = &f
	}
	page, err := t.store.ListAgentJobs(ctx, chat.UserID, pageNum, limit, filters)
	if err != nil {
		return t.fail(listKindJobs, fmt.Sprintf("failed to list jobs: %v", err))
	}
	items := make([]listItem, 0, len(page.Results))
	for _, r := range page.Results {
		j, ok := r.(*models.AgentJob)
		if !ok || j == nil {
			continue
		}
		name := ""
		if j.Title != nil {
			name = *j.Title
		}
		item := listItem{
			ID:     j.ID.String(),
			Name:   name,
			Status: string(j.Status),
		}
		// Next run only for jobs that still have a future — schedule_input ("in 5 minutes")
		// is stale nonsense next to status=complete/failed.
		if j.Status != models.AgentJobStatusComplete && j.Status != models.AgentJobStatusFailed {
			item.NextRuntime = rfc3339Ptr(j.NextRunAt)
		}
		items = append(items, item)
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	activeNote := ""
	if !a.IncludeCompleted {
		activeNote = "Active jobs only; pass include_completed=true to also see finished/failed one-offs."
	}
	note := joinNotes(activeNote, listTruncationNote("jobs", meta, len(items)))
	return t.okPaged(listKindJobs, items, note, meta)
}

func (t *ListTool) listMCPServers(ctx context.Context, chat *models.Chat) (string, error) {
	servers, err := t.store.ListChatMCPServers(ctx, chat.UserID, chat.ID)
	if err != nil {
		return t.fail(listKindMCPServers, fmt.Sprintf("failed to list MCP servers: %v", err))
	}
	loadedByServer, err := t.store.ListChatMCPLoadedTools(ctx, chat.UserID, chat.ID)
	if err != nil {
		return t.fail(listKindMCPServers, fmt.Sprintf("failed to list loaded MCP tools: %v", err))
	}
	toolsByServer := map[uuid.UUID][]string{}
	discoveryErrors := map[uuid.UUID]string{}
	if t.mcpDiscoverer != nil && len(servers) > 0 {
		out, discoverErr := t.mcpDiscoverer.DiscoverTools(ctx, chat.UserID, servers)
		for _, tool := range out.Tools {
			toolsByServer[tool.ConnectorID] = append(toolsByServer[tool.ConnectorID], tool.FullName)
		}
		for id, msg := range out.Errors {
			discoveryErrors[id] = strings.TrimSpace(msg)
		}
		if discoverErr != nil && t.logger != nil {
			t.logger.Warn("list mcp_servers discovery had no healthy connectors", zap.Error(discoverErr))
		}
	}
	items := make([]listItem, 0, len(servers))
	failedDiscovery := 0
	for _, s := range servers {
		if s == nil {
			continue
		}
		status := strings.TrimSpace(s.Status)
		if status == "" {
			status = models.MCPServerStatusActive
		}
		if status == models.MCPServerStatusInvalid {
			failedDiscovery++
		}
		loadedTools := loadedByServer[s.ID]
		knownTools := toolsByServer[s.ID]
		if len(knownTools) > 1 {
			sort.Strings(knownTools)
		}
		if len(loadedTools) > 1 {
			sort.Strings(loadedTools)
		}
		if status != models.MCPServerStatusInvalid && strings.TrimSpace(discoveryErrors[s.ID]) != "" {
			failedDiscovery++
		}
		items = append(items, listItem{
			ID:           s.ID.String(),
			Name:         s.Name,
			Description:  s.Description,
			URL:          s.ServerURL,
			Status:       status,
			StatusDetail: agentFriendlyMCPStatusDetail(status),
			MCPTools:     knownTools,
			LoadedTools:  loadedTools,
		})
	}
	result := listResult{
		Kind:  listKindMCPServers,
		Count: len(items),
		Items: items,
		Note:  "MCP servers connected to the current conversation. Use load_mcp_tools before calling MCP tools.",
	}
	if len(items) > 0 && failedDiscovery == len(items) {
		result.Error = "MCP discovery failed for all connected servers. MCP tools are currently unavailable; review connector auth/settings and retry."
	}
	if len(items) > 0 && failedDiscovery > 0 && failedDiscovery < len(items) {
		result.Note = joinNotes(result.Note, fmt.Sprintf("%d server(s) are currently unavailable for MCP tool discovery.", failedDiscovery))
	}
	return marshalToolResult(result, listToolName)
}

func agentFriendlyMCPStatusDetail(status string) string {
	switch strings.TrimSpace(status) {
	case models.MCPServerStatusActive:
		return "Connector is healthy and available for tool discovery."
	case models.MCPServerStatusRefreshError:
		return "Connector is reachable, but authentication refresh needs attention."
	case models.MCPServerStatusInvalid:
		return "Connector discovery failed. Check authentication or server configuration."
	case models.MCPServerStatusDisabled:
		return "Connector is disabled and will not be used by the agent."
	default:
		return "Connector status is unknown."
	}
}

// normalizeFileScope maps a raw scope string to a known value (default: all).
func normalizeFileScope(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case listFileScopePersonality:
		return listFileScopePersonality
	case listFileScopeConversation, listFileScopeThreadAlias:
		return listFileScopeConversation
	default:
		return listFileScopeAll
	}
}

// listWorkspace lists the agent's workspace files for this conversation: chat/ then agent/.
// filter, when given, is a path prefix ("agent/", "chat/notes").
func (t *ListTool) listWorkspace(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	if t.workspace == nil {
		return t.fail(listKindWorkspace, "the workspace is not available here")
	}
	limit := clampLimit(a.Limit, listDefaultLimit)
	files, err := t.workspace.listForChat(ctx, chat, a.Filter, limit+1)
	if err != nil {
		return t.fail(listKindWorkspace, fmt.Sprintf("failed to list workspace files: %v", err))
	}
	note := ""
	if len(files) > limit {
		files = files[:limit]
		note = "More files exist; narrow with filter (a path prefix) or raise limit."
	}
	items := make([]listItem, 0, len(files))
	for _, f := range files {
		items = append(items, listItem{
			ID:        f.ID.String(),
			Name:      f.Root + "/" + f.Path,
			FileType:  f.ContentType,
			Revision:  f.CurrentRevision,
			Size:      f.Size,
			UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	if len(items) == 0 && note == "" {
		note = "No workspace files yet. Create one with write_file, e.g. agent/notes.md."
	}
	return t.ok(listKindWorkspace, items, note)
}
