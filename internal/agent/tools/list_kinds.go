package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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
	items := make([]listItem, 0, len(servers))
	for _, s := range servers {
		if s == nil {
			continue
		}
		status := "ok"
		if s.ErrorMessage != "" {
			status = "error: " + s.ErrorMessage
		}
		items = append(items, listItem{
			ID:          s.ID.String(),
			Name:        s.Name,
			Description: s.Description,
			URL:         s.ServerURL,
			Status:      status,
		})
	}
	return t.ok(listKindMCPServers, items, "MCP servers connected to the current conversation.")
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
