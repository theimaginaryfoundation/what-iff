package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeListStore struct {
	modelsList []*models.Model
	pers       []*models.Personality
	rituals    []*models.Ritual
	files      []*models.FileAttachment
	filesTotal int
	chats      []*models.Chat
	jobs       []*models.AgentJob
	mcp        []*models.MCPServer

	// captured inputs
	lastFileFilters models.FileAttachmentFilters
	lastChatFilters models.ChatFilters
	lastJobFilters  models.AgentJobFilters
	lastPageNum     int
	lastPageSize    int
	lastMCPChatID   uuid.UUID
}

func (f *fakeListStore) ListModels(_ context.Context) ([]*models.Model, error) {
	return f.modelsList, nil
}
func (f *fakeListStore) ListPersonalities(_ context.Context, _ uuid.UUID, pageNum, pageSize int, _ models.PersonalityFilters) (*models.PaginatedResponse, error) {
	f.lastPageNum, f.lastPageSize = pageNum, pageSize
	return paginatePage(toAny(f.pers), pageNum, pageSize), nil
}
func (f *fakeListStore) ListRituals(_ context.Context, _ uuid.UUID, pageNum, pageSize int, _ models.RitualFilters) (*models.PaginatedResponse, error) {
	f.lastPageNum, f.lastPageSize = pageNum, pageSize
	return paginatePage(toAny(f.rituals), pageNum, pageSize), nil
}
func (f *fakeListStore) ListFileAttachments(_ context.Context, _ uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	f.lastFileFilters = filters
	f.lastPageNum, f.lastPageSize = pageNum, pageSize
	res := paginatePage(toAny(f.files), pageNum, pageSize)
	if f.filesTotal > 0 {
		res.TotalCount = f.filesTotal
	}
	return res, nil
}
func (f *fakeListStore) ListChats(_ context.Context, _ uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
	f.lastChatFilters = filters
	f.lastPageNum, f.lastPageSize = pageNum, pageSize
	chats := f.chats
	if filters.HasMessages != nil && *filters.HasMessages {
		filtered := make([]*models.Chat, 0, len(chats))
		for _, c := range chats {
			if c != nil && c.LastMessageTime != nil {
				filtered = append(filtered, c)
			}
		}
		chats = filtered
	}
	return paginatePage(toAny(chats), pageNum, pageSize), nil
}
func (f *fakeListStore) ListAgentJobs(_ context.Context, _ uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error) {
	f.lastJobFilters = filters
	f.lastPageNum, f.lastPageSize = pageNum, pageSize
	return paginatePage(toAny(f.jobs), pageNum, pageSize), nil
}
func (f *fakeListStore) ListChatMCPServers(_ context.Context, _, chatID uuid.UUID) ([]*models.MCPServer, error) {
	f.lastMCPChatID = chatID
	return f.mcp, nil
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func paginatePage(items []any, pageNum, pageSize int) *models.PaginatedResponse {
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	total := len(items)
	start := (pageNum - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return &models.PaginatedResponse{
		Results:    items[start:end],
		TotalCount: total,
		Page:       pageNum,
	}
}

func newTestListTool(store listStore) *ListTool {
	return &ListTool{store: store, logger: zap.NewNop()}
}

func listTestChat() *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
}

func decodeList(t *testing.T, out string) listResult {
	t.Helper()
	var res listResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("failed to decode list result: %v (%s)", err, out)
	}
	return res
}

func runList(t *testing.T, tool *ListTool, args string) listResult {
	t.Helper()
	out, err := tool.List(context.Background(), listTestChat(), []byte(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return decodeList(t, out)
}

func TestListRequiresKind(t *testing.T) {
	res := runList(t, newTestListTool(&fakeListStore{}), `{}`)
	if res.Error == "" {
		t.Fatal("expected error when kind is omitted")
	}
}

func TestListUnknownKind(t *testing.T) {
	res := runList(t, newTestListTool(&fakeListStore{}), `{"kind":"widgets"}`)
	if res.Error == "" {
		t.Fatal("expected error for unknown kind")
	}
}

func TestListModels(t *testing.T) {
	store := &fakeListStore{modelsList: []*models.Model{
		{ID: uuid.New(), Name: "opus", DisplayName: "Opus", Provider: "anthropic", ToolSupport: true},
	}}
	res := runList(t, newTestListTool(store), `{"kind":"models"}`)
	if res.Kind != listKindModels || res.Count != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Items[0].Name != "Opus" || res.Items[0].Provider != "anthropic" || res.Items[0].ToolSupport == nil || !*res.Items[0].ToolSupport {
		t.Fatalf("unexpected model item: %+v", res.Items[0])
	}
}

func TestListFilesAppliesFileTypeAndScope(t *testing.T) {
	chat := listTestChat()
	store := &fakeListStore{
		files:      []*models.FileAttachment{{ID: uuid.New(), Name: "cat.png", FileType: "image/png"}},
		filesTotal: 12,
	}
	tool := newTestListTool(store)

	out, err := tool.List(context.Background(), chat, []byte(`{"kind":"files","file_type":"image","scope":"personality","limit":5}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeList(t, out)
	if res.Count != 1 || res.Items[0].FileType != "image/png" {
		t.Fatalf("unexpected file item: %+v", res)
	}
	// file_type propagated to the datastore filter.
	if store.lastFileFilters.FileType == nil || *store.lastFileFilters.FileType != "image" {
		t.Fatalf("expected file_type filter 'image', got %+v", store.lastFileFilters.FileType)
	}
	// personality scope -> PersonalityID set + DocsOnly.
	if store.lastFileFilters.PersonalityID == nil || *store.lastFileFilters.PersonalityID != chat.PersonalityID {
		t.Fatalf("expected personality scope to set PersonalityID, got %+v", store.lastFileFilters.PersonalityID)
	}
	if store.lastFileFilters.DocsOnly == nil || !*store.lastFileFilters.DocsOnly {
		t.Fatalf("expected DocsOnly for personality scope")
	}
	if store.lastPageNum != 1 || store.lastPageSize != 5 {
		t.Fatalf("expected page=1 limit=5, got page=%d size=%d", store.lastPageNum, store.lastPageSize)
	}
	// truncation note + has_more when TotalCount exceeds returned.
	if res.Note == "" || !res.HasMore || res.TotalCount != 12 || res.Page != 1 {
		t.Fatalf("expected truncation pagination metadata, got %+v", res)
	}
}

func TestListFilesConversationScope(t *testing.T) {
	store := &fakeListStore{files: []*models.FileAttachment{{ID: uuid.New(), Name: "doc.pdf", FileType: "application/pdf"}}}
	tool := newTestListTool(store)
	_, err := tool.List(context.Background(), listTestChat(), []byte(`{"kind":"files","scope":"conversation"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastFileFilters.GlobalOnly == nil || !*store.lastFileFilters.GlobalOnly {
		t.Fatalf("expected GlobalOnly for conversation scope, got %+v", store.lastFileFilters.GlobalOnly)
	}
}

func TestNormalizeFileScopeAcceptsThreadAlias(t *testing.T) {
	if got := normalizeFileScope(listFileScopeThreadAlias); got != listFileScopeConversation {
		t.Fatalf("thread alias normalized to %q, want %q", got, listFileScopeConversation)
	}
}

func TestListConversationsPagination(t *testing.T) {
	now := time.Now()
	chats := make([]*models.Chat, 0, 5)
	for i := 0; i < 5; i++ {
		chats = append(chats, &models.Chat{ID: uuid.New(), Name: "c", LastMessageTime: &now})
	}
	store := &fakeListStore{chats: chats}
	res := runList(t, newTestListTool(store), `{"kind":"conversations","limit":2,"page":2}`)
	if store.lastPageNum != 2 || store.lastPageSize != 2 {
		t.Fatalf("expected page=2 limit=2 passed to store, got page=%d size=%d", store.lastPageNum, store.lastPageSize)
	}
	if res.Count != 2 || res.Page != 2 || res.TotalCount != 5 || !res.HasMore {
		t.Fatalf("unexpected page 2 result: %+v", res)
	}
	if res.Note == "" || !strings.Contains(res.Note, "page=3") {
		t.Fatalf("expected note pointing at page=3, got %q", res.Note)
	}

	last := runList(t, newTestListTool(store), `{"kind":"conversations","limit":2,"page":3}`)
	if last.Count != 1 || last.HasMore {
		t.Fatalf("expected final page without has_more: %+v", last)
	}
}

func TestListJobsDefaultsToActive(t *testing.T) {
	title := "daily standup"
	next := time.Now().Add(24 * time.Hour)
	store := &fakeListStore{jobs: []*models.AgentJob{
		{ID: uuid.New(), Title: &title, Status: models.AgentJobStatusActive, ScheduleInput: "every day at 9am", NextRunAt: &next},
	}}
	tool := newTestListTool(store)

	// Default: active-only filter applied.
	out, _ := tool.List(context.Background(), listTestChat(), []byte(`{"kind":"jobs"}`))
	res := decodeList(t, out)
	if store.lastJobFilters.Status == nil || *store.lastJobFilters.Status != models.AgentJobStatusActive {
		t.Fatalf("expected active status filter by default, got %+v", store.lastJobFilters.Status)
	}
	if res.Items[0].Name != title || res.Items[0].NextRuntime == "" {
		t.Fatalf("unexpected job item: %+v", res.Items[0])
	}

	// include_completed drops the status filter.
	_, _ = tool.List(context.Background(), listTestChat(), []byte(`{"kind":"jobs","include_completed":true}`))
	if store.lastJobFilters.Status != nil {
		t.Fatalf("expected no status filter with include_completed, got %+v", store.lastJobFilters.Status)
	}
}

func TestListJobsOmitsNextRunWhenTerminal(t *testing.T) {
	title := "one-shot"
	staleNext := time.Now().Add(5 * time.Minute)
	store := &fakeListStore{jobs: []*models.AgentJob{
		{ID: uuid.New(), Title: &title, Status: models.AgentJobStatusComplete, ScheduleInput: "in 5 minutes", NextRunAt: &staleNext},
	}}
	res := runList(t, newTestListTool(store), `{"kind":"jobs","include_completed":true}`)
	if res.Count != 1 {
		t.Fatalf("expected 1 job, got %+v", res)
	}
	if res.Items[0].NextRuntime != "" {
		t.Fatalf("expected no next_runtime for completed job, got %q", res.Items[0].NextRuntime)
	}
	if res.Items[0].Status != string(models.AgentJobStatusComplete) {
		t.Fatalf("unexpected status: %+v", res.Items[0])
	}
}

func TestListConversations(t *testing.T) {
	now := time.Now()
	archived := true
	store := &fakeListStore{chats: []*models.Chat{
		{ID: uuid.New(), Name: "empty shell"},                                          // no LastMessageTime → hidden
		{ID: uuid.New(), Name: "planning", LastMessageTime: &now, Archived: &archived}, // real conversation
	}}
	res := runList(t, newTestListTool(store), `{"kind":"conversations"}`)
	if store.lastChatFilters.HasMessages == nil || !*store.lastChatFilters.HasMessages {
		t.Fatalf("expected HasMessages=true filter, got %+v", store.lastChatFilters.HasMessages)
	}
	if !store.lastChatFilters.IncludeArchived {
		t.Fatal("expected conversations list to include archived threads")
	}
	if res.Items[0].Archived == nil || !*res.Items[0].Archived {
		t.Fatalf("expected archived state on the conversation item, got %+v", res.Items[0])
	}
	if res.Kind != listKindConversations || res.Count != 1 || res.Items[0].Name != "planning" {
		t.Fatalf("unexpected conversations result: %+v", res)
	}
	if res.Items[0].UpdatedAt == "" {
		t.Fatalf("expected updated_at from LastMessageTime")
	}
}

func TestListWhitespaceOnlyFilterIgnored(t *testing.T) {
	now := time.Now()
	title := "job"
	store := &fakeListStore{
		files: []*models.FileAttachment{{ID: uuid.New(), Name: "a.png", FileType: "image/png"}},
		chats: []*models.Chat{{ID: uuid.New(), Name: "planning", LastMessageTime: &now}},
		jobs:  []*models.AgentJob{{ID: uuid.New(), Title: &title, Status: models.AgentJobStatusActive}},
	}
	tool := newTestListTool(store)

	for _, args := range []string{
		`{"kind":"files","filter":"   "}`,
		`{"kind":"conversations","filter":"\t\n"}`,
		`{"kind":"jobs","filter":" "}`,
	} {
		if _, err := tool.List(context.Background(), listTestChat(), []byte(args)); err != nil {
			t.Fatalf("unexpected error for %s: %v", args, err)
		}
	}
	if store.lastFileFilters.Name != nil {
		t.Fatalf("whitespace filter must not set file Name, got %q", *store.lastFileFilters.Name)
	}
	if store.lastChatFilters.Query != nil {
		t.Fatalf("whitespace filter must not set chat Query, got %q", *store.lastChatFilters.Query)
	}
	if store.lastJobFilters.Query != nil {
		t.Fatalf("whitespace filter must not set job Query, got %q", *store.lastJobFilters.Query)
	}
}

func TestListMCPServersScopedToChat(t *testing.T) {
	chat := listTestChat()
	store := &fakeListStore{mcp: []*models.MCPServer{
		{ID: uuid.New(), Name: "github", ServerURL: "https://mcp.example", ErrorMessage: "auth failed"},
	}}
	tool := newTestListTool(store)
	out, err := tool.List(context.Background(), chat, []byte(`{"kind":"mcp_servers"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastMCPChatID != chat.ID {
		t.Fatalf("expected MCP listing scoped to current chat %s, got %s", chat.ID, store.lastMCPChatID)
	}
	res := decodeList(t, out)
	if res.Items[0].Status != "error: auth failed" || res.Items[0].URL != "https://mcp.example" {
		t.Fatalf("unexpected mcp item: %+v", res.Items[0])
	}
}

func TestListFilesPersonalityScopeWithoutPersonality(t *testing.T) {
	store := &fakeListStore{}
	tool := newTestListTool(store)
	// Chat with no personality -> personality scope yields a note, not a bogus filter.
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()} // PersonalityID zero
	out, err := tool.List(context.Background(), chat, []byte(`{"kind":"files","scope":"personality"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeList(t, out)
	if res.Count != 0 || res.Note == "" {
		t.Fatalf("expected empty result + note when no active personality, got %+v", res)
	}
}
