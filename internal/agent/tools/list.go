package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// List kinds — the verbs the agent selects between.
const (
	listKindModels        = "models"
	listKindPersonalities = "personalities"
	listKindSkills        = "skills"
	listKindFiles         = "files"
	listKindConversations = "conversations"
	listKindJobs          = "jobs"
	listKindMCPServers    = "mcp_servers"
)

// File scope values (files kind).
const (
	listFileScopeAll          = "all"
	listFileScopePersonality  = "personality"
	listFileScopeConversation = "conversation"
	// listFileScopeThreadAlias remains accepted on input for callers that used the first
	// unreleased list-tool revision; the schema intentionally advertises only "conversation".
	listFileScopeThreadAlias = "thread"
)

const (
	listDefaultLimit   = 50
	listMaxLimit       = 200
	listDiscoveryLimit = 100 // personalities / skills (small, stable sets)
	listToolName       = "list"
)

// ListDescription is the agent-facing description. One tool, one `kind` verb, so the model stops
// juggling a separate list_* tool per resource (and the tool surface stays small).
const ListDescription = `List your available resources. Pick a kind:

- models — chat models you can run (id, name, provider, tool support).
- personalities — personalities you can switch to or run as a subagent.
- skills — skills you can attach to a subagent or scheduled job (id, name, description).
- files — your uploaded files, most-recent first. Filter with file_type (e.g. "image", "pdf") and scope ("personality" docs, "conversation" attachments, or "all"). Images dominate most libraries, so pass file_type to cut through them.
- conversations — your past conversations, including archived imports, that have messages, most-recent first (id, name). Pass an id to find_context (mode="conversation"/"origin") to read one.
- jobs — your scheduled jobs (id, name, status, next_runtime for still-active ones). Active by default; set include_completed=true to also see finished/failed one-offs.
- mcp_servers — MCP servers connected to the current conversation.

Use filter for a free-text name/content match (files, conversations, jobs). Use limit to cap page size and page (1-based) to walk further results when has_more is true.`

// ListToolSpec is the shared function-tool shape for the unified list tool. Co-located with the
// implementation (like recall) since it's a self-contained feature.
var ListToolSpec = FunctionToolSpec{
	Name:        listToolName,
	Description: ListDescription,
	Properties: map[string]interface{}{
		"kind": map[string]interface{}{
			"type":        "string",
			"enum":        []string{listKindModels, listKindPersonalities, listKindSkills, listKindFiles, listKindConversations, listKindJobs, listKindMCPServers},
			"description": "Which resource to list. Required.",
		},
		"filter": map[string]interface{}{
			"type":        "string",
			"description": "Free-text name/content match. Applies to files, conversations, and jobs.",
		},
		"file_type": map[string]interface{}{
			"type":        "string",
			"description": "files only: substring match on MIME type, e.g. 'image', 'pdf', 'text'. Use to include or skip image-heavy libraries.",
		},
		"scope": map[string]interface{}{
			"type":        "string",
			"enum":        []string{listFileScopeAll, listFileScopePersonality, listFileScopeConversation},
			"description": "files only: 'personality' (this personality's docs), 'conversation' (conversation uploads), or 'all' (default).",
		},
		"include_completed": map[string]interface{}{
			"type":        "boolean",
			"description": "jobs only: when true, also include finished/failed jobs. Default false (active scheduled jobs only).",
		},
		"limit": map[string]interface{}{
			"type":        "integer",
			"description": "Page size for files/conversations/jobs (also personalities/skills). Default 50.",
			"minimum":     1,
			"maximum":     listMaxLimit,
			"default":     listDefaultLimit,
		},
		"page": map[string]interface{}{
			"type":        "integer",
			"description": "1-based page for files/conversations/jobs (also personalities/skills). Default 1. When has_more is true, call again with page+1.",
			"minimum":     1,
			"default":     1,
		},
	},
	// `kind` has no sensible default, so it is genuinely required (validated in List too).
	Required: []string{"kind"},
}

// listStore is the narrow datastore surface the list tool needs. *datastore.Datastore satisfies it.
type listStore interface {
	ListModels(ctx context.Context) ([]*models.Model, error)
	ListPersonalities(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error)
	ListRituals(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.RitualFilters) (*models.PaginatedResponse, error)
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	ListChats(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error)
	ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error)
	ListChatMCPServers(ctx context.Context, userID, chatID uuid.UUID) ([]*models.MCPServer, error)
}

// ListTool implements the unified `list` agent tool.
type ListTool struct {
	store  listStore
	logger *zap.Logger
}

// NewListTool constructs a ListTool backed by the datastore.
func NewListTool(ds *datastore.Datastore, logger *zap.Logger) *ListTool {
	return &ListTool{store: ds, logger: logger}
}

// --- argument + result types ---------------------------------------------

type listArgs struct {
	Kind             string `json:"kind"`
	Filter           string `json:"filter,omitempty"`
	FileType         string `json:"file_type,omitempty"`
	Scope            string `json:"scope,omitempty"`
	IncludeCompleted bool   `json:"include_completed,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Page             int    `json:"page,omitempty"`
}

// listItem is a single uniform row. Fields are populated per kind and omitted when empty, so every
// kind reads through one shape while only carrying what's relevant.
type listItem struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Provider    string `json:"provider,omitempty"`     // models
	ToolSupport *bool  `json:"tool_support,omitempty"` // models
	FileType    string `json:"file_type,omitempty"`    // files
	Status      string `json:"status,omitempty"`       // jobs, mcp_servers
	NextRuntime string `json:"next_runtime,omitempty"` // jobs (omitted when complete/failed)
	UpdatedAt   string `json:"updated_at,omitempty"`   // personalities, conversations
	URL         string `json:"url,omitempty"`          // mcp_servers
	Archived    *bool  `json:"archived,omitempty"`     // conversations
}

type listResult struct {
	Kind       string     `json:"kind"`
	Count      int        `json:"count"`
	Page       int        `json:"page,omitempty"`
	TotalCount int        `json:"total_count,omitempty"`
	HasMore    bool       `json:"has_more,omitempty"`
	Items      []listItem `json:"items"`
	Note       string     `json:"note,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// listPageMeta is set for kinds that page through the datastore.
type listPageMeta struct {
	Page       int
	Limit      int
	TotalCount int
}

// --- dispatch -------------------------------------------------------------

// List executes a list tool call, routing on `kind`.
func (t *ListTool) List(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	var a listArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return t.fail("", fmt.Sprintf("invalid arguments: %v", err))
	}

	kind := strings.ToLower(strings.TrimSpace(a.Kind))
	switch kind {
	case listKindModels:
		return t.listModels(ctx)
	case listKindPersonalities:
		return t.listPersonalities(ctx, chat, a)
	case listKindSkills:
		return t.listSkills(ctx, chat, a)
	case listKindFiles:
		return t.listFiles(ctx, chat, a)
	case listKindConversations:
		return t.listConversations(ctx, chat, a)
	case listKindJobs:
		return t.listJobs(ctx, chat, a)
	case listKindMCPServers:
		return t.listMCPServers(ctx, chat)
	case "":
		return t.fail("", "kind is required; one of: models, personalities, skills, files, conversations, jobs, mcp_servers")
	default:
		return t.fail(kind, fmt.Sprintf("unknown kind %q; expected one of: models, personalities, skills, files, conversations, jobs, mcp_servers", a.Kind))
	}
}

func (t *ListTool) fail(kind, msg string) (string, error) {
	return marshalToolResult(listResult{Kind: kind, Error: msg}, listToolName)
}

func (t *ListTool) ok(kind string, items []listItem, note string) (string, error) {
	return t.okPaged(kind, items, note, listPageMeta{})
}

func (t *ListTool) okPaged(kind string, items []listItem, note string, meta listPageMeta) (string, error) {
	if items == nil {
		items = []listItem{}
	}
	res := listResult{Kind: kind, Count: len(items), Items: items, Note: note}
	if meta.Page > 0 {
		res.Page = meta.Page
		res.TotalCount = meta.TotalCount
		shownThrough := (meta.Page-1)*meta.Limit + len(items)
		res.HasMore = shownThrough < meta.TotalCount
	}
	return marshalToolResult(res, listToolName)
}

// clampLimit resolves a requested limit against a default and hard maximum.
func clampLimit(requested, def int) int {
	n := requested
	if n <= 0 {
		n = def
	}
	if n > listMaxLimit {
		n = listMaxLimit
	}
	return n
}

// clampPage resolves a 1-based page number (default 1).
func clampPage(requested int) int {
	if requested < 1 {
		return 1
	}
	return requested
}

// listTruncationNote tells the agent how to continue when results are capped.
func listTruncationNote(resource string, meta listPageMeta, shown int) string {
	if meta.TotalCount <= 0 || (meta.Page == 1 && shown >= meta.TotalCount) {
		return ""
	}
	base := fmt.Sprintf("Showing %d of %d %s (page %d)", shown, meta.TotalCount, resource, meta.Page)
	shownThrough := (meta.Page-1)*meta.Limit + shown
	if shownThrough < meta.TotalCount {
		return base + fmt.Sprintf("; pass page=%d for more.", meta.Page+1)
	}
	return base + "."
}

func joinNotes(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func boolPtr(b bool) *bool { return &b }

func rfc3339Ptr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
