package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
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

// listSandboxFiles lists the files uploaded to this conversation: the only files a sandboxed chat
// can see, whatever scope was asked for. Name and type filters still apply.
//
// The scope is one conversation's uploads, and ListFileAttachmentsInChatScope caps it (newest
// first, at most maxFilesInChatScope rows), so filtering and paging the slice here is bounded and
// stable for the call; it is not the account-wide library listFiles pages in SQL.
func (t *ListTool) listSandboxFiles(ctx context.Context, chat *models.Chat, a listArgs, limit, pageNum int) (string, error) {
	scoped, err := t.store.ListFileAttachmentsInChatScope(ctx, chat.UserID, chat.ID, nil, 0)
	if err != nil {
		return t.fail(listKindFiles, fmt.Sprintf("failed to list files: %v", err))
	}
	name, _ := validateNonEmptyString(a.Filter)
	fileType, _ := validateNonEmptyString(a.FileType)
	matched := make([]*models.FileAttachment, 0, len(scoped))
	for _, fa := range scoped {
		if fa == nil {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(fa.Name), strings.ToLower(name)) {
			continue
		}
		if fileType != "" && !strings.EqualFold(fa.FileType, fileType) {
			continue
		}
		matched = append(matched, fa)
	}
	start := (pageNum - 1) * limit
	if start > len(matched) {
		start = len(matched)
	}
	end := start + limit
	if end > len(matched) {
		end = len(matched)
	}
	items := make([]listItem, 0, end-start)
	for _, fa := range matched[start:end] {
		items = append(items, listItem{ID: fa.ID.String(), Name: fa.Name, FileType: fa.FileType})
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: len(matched)}
	return t.okPaged(listKindFiles, items, listTruncationNote("files", meta, len(items)), meta)
}

func (t *ListTool) listFiles(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	view := strings.ToLower(strings.TrimSpace(a.View))
	if view != "" && view != listFileViewList && view != listFileViewTree {
		return t.fail(listKindFiles, fmt.Sprintf("unknown view %q; use \"list\" or \"tree\"", a.View))
	}
	// A tree is drawn from one page, so it defaults to the largest page: one folder's whole tree
	// in most libraries.
	defLimit := listDefaultLimit
	if view == listFileViewTree {
		defLimit = listMaxLimit
	}
	limit := clampLimit(a.Limit, defLimit)
	pageNum := clampPage(a.Page)
	if chat.IsSandboxed() {
		return t.listSandboxFiles(ctx, chat, a, limit, pageNum)
	}
	filters := models.FileAttachmentFilters{}
	if ft, ok := validateNonEmptyString(a.FileType); ok {
		filters.FileType = &ft
	}
	if f, ok := validateNonEmptyString(a.Filter); ok {
		filters.Name = &f
	}
	if raw, ok := validateNonEmptyString(a.Folder); ok {
		folder, err := models.NormalizeFolder(raw)
		if err != nil {
			return t.fail(listKindFiles, fmt.Sprintf("invalid folder: %v", err))
		}
		if folder != "" { // "/" or "" would mean the top level, which is "no folder filter" here
			filters.FolderPrefix = &folder
		}
	}

	switch origin := models.FileOrigin(strings.ToLower(strings.TrimSpace(a.Origin))); origin {
	case "":
	case models.FileOriginAgent, models.FileOriginUser:
		filters.Origin = origin
	default:
		return t.fail(listKindFiles, fmt.Sprintf("unknown origin %q; use \"agent\" or \"user\"", a.Origin))
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
		item := listItem{
			ID:       fa.ID.String(),
			Name:     fa.Name,
			FileType: fa.FileType,
			Folder:   fa.Folder,
		}
		if fa.Origin != "" && fa.Origin != models.FileOriginUser {
			item.Origin = string(fa.Origin)
		}
		items = append(items, item)
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	if view == listFileViewTree {
		res := pagedListResult(listKindFiles, items, listTruncationNote("files", meta, len(items)), meta)
		res.Tree = renderFileTree(items)
		res.Items = []listItem{}
		return marshalToolResult(res, listToolName)
	}
	return t.okPaged(listKindFiles, items, listTruncationNote("files", meta, len(items)), meta)
}

// listConversations deliberately spans active and archived threads so the agent can
// discover imported history and read it through find_context without rehydration.
func (t *ListTool) listConversations(ctx context.Context, chat *models.Chat, a listArgs) (string, error) {
	if chat.IsSandboxed() {
		// A sandbox reads only itself, so the only conversation it can list (and then read with
		// find_context) is this one; nothing else is named, filtered or paged.
		items := []listItem{{ID: chat.ID.String(), Name: chat.Name, UpdatedAt: rfc3339Ptr(chat.LastMessageTime), Archived: chat.Archived}}
		return t.ok(listKindConversations, items, "This sandboxed conversation can only read itself; this is the one conversation available to find_context.")
	}
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
	// A job id as the filter reads that one job in full, whatever its status: rows otherwise show only
	// the start of each prompt, and a model about to rewrite a prompt needs the whole of it.
	if id, err := uuid.Parse(strings.TrimSpace(a.Filter)); err == nil {
		return t.getJob(ctx, chat, id)
	}
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
		item := jobListItem(j)
		item.Description = TruncateRunes(item.Description, listJobPromptRunes)
		items = append(items, item)
	}
	meta := listPageMeta{Page: pageNum, Limit: limit, TotalCount: page.TotalCount}
	activeNote := ""
	if !a.IncludeCompleted {
		activeNote = "Active jobs only; pass include_completed=true to also see paused jobs and finished/failed one-offs."
	}
	note := joinNotes(activeNote, listJobPromptNote, listTruncationNote("jobs", meta, len(items)))
	return t.okPaged(listKindJobs, items, note, meta)
}

// getJob lists the one job id names, with its whole prompt, model and skills.
func (t *ListTool) getJob(ctx context.Context, chat *models.Chat, id uuid.UUID) (string, error) {
	j, err := t.store.GetAgentJob(ctx, chat.UserID, id)
	if errors.Is(err, datastore.ErrAgentJobNotFound) {
		return t.ok(listKindJobs, nil, "No job has that id; list jobs (include_completed=true) to find it.")
	}
	if err != nil {
		return t.fail(listKindJobs, fmt.Sprintf("failed to load job: %v", err))
	}
	item := jobListItem(j)
	if j.ModelID != nil {
		item.ModelID = j.ModelID.String()
	}
	for _, r := range j.Rituals {
		if r != nil {
			item.SkillIDs = append(item.SkillIDs, r.ID.String())
		}
	}
	sort.Strings(item.SkillIDs)
	return t.ok(listKindJobs, []listItem{item}, "The job in full: description is its whole prompt.")
}

// jobListItem is a job's row with its prompt uncut; the list view cuts it to a preview.
func jobListItem(j *models.AgentJob) listItem {
	item := listItem{
		ID:          j.ID.String(),
		Description: strings.TrimSpace(j.Prompt),
		Status:      string(j.Status),
		Schedule:    j.ScheduleInput,
	}
	if j.Title != nil {
		item.Name = *j.Title
	}
	if j.ChatID != nil {
		item.ThreadID = j.ChatID.String()
	}
	// Next run only for jobs that still have a future — schedule_input ("in 5 minutes")
	// is stale nonsense next to status=complete/failed.
	if j.Status != models.AgentJobStatusComplete && j.Status != models.AgentJobStatusFailed {
		item.NextRuntime = rfc3339Ptr(j.NextRunAt)
	}
	return item
}

func (t *ListTool) listMCPServers(ctx context.Context, chat *models.Chat, scope MCPScopeFunc) (string, error) {
	var servers []*models.MCPServer
	var err error
	if scope != nil {
		servers, err = scope(ctx)
	} else {
		servers, err = t.store.ListChatMCPServers(ctx, chat.UserID, chat.ID)
	}
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
		Note:  "MCP servers available to the current conversation (attached to it or linked to an active skill). Use load_tools before calling MCP tools.",
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
