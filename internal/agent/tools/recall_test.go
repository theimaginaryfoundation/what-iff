package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// fakeDistiller is a stand-in RecallDistiller: it records what it was handed and returns a fixed
// answer (or error), so investigate's positive/fallback wiring can be tested without a real model.
type fakeDistiller struct {
	answer      string
	err         error
	gotQuestion string
	gotMaterial string
	calls       int
}

func (f *fakeDistiller) Distill(_ context.Context, question, material string) (string, error) {
	f.calls++
	f.gotQuestion = question
	f.gotMaterial = material
	return f.answer, f.err
}

// --- time scope parsing ---------------------------------------------------

func TestParseTimeScope(t *testing.T) {
	now := time.Date(2025, 8, 5, 12, 0, 0, 0, time.UTC)

	t.Run("empty is no window", func(t *testing.T) {
		w, err := parseTimeScope("", now)
		if err != nil || w.Min != nil || w.Max != nil {
			t.Fatalf("expected empty window, got %+v err=%v", w, err)
		}
	})

	t.Run("last N days", func(t *testing.T) {
		w, err := parseTimeScope("last 7 days", now)
		if err != nil || w.Min == nil {
			t.Fatalf("expected min set, got %+v err=%v", w, err)
		}
		if got := now.Sub(*w.Min); got != 7*24*time.Hour {
			t.Fatalf("expected 7d window, got %v", got)
		}
	})

	t.Run("before date", func(t *testing.T) {
		w, err := parseTimeScope("before 2025-01-01", now)
		if err != nil || w.Max == nil || w.Min != nil {
			t.Fatalf("expected max-only, got %+v err=%v", w, err)
		}
	})

	t.Run("between normalizes order", func(t *testing.T) {
		w, err := parseTimeScope("between 2025-03-01 and 2025-01-01", now)
		if err != nil || w.Min == nil || w.Max == nil {
			t.Fatalf("expected both bounds, got %+v err=%v", w, err)
		}
		if w.Min.After(*w.Max) {
			t.Fatalf("expected Min<=Max after normalization, got min=%v max=%v", w.Min, w.Max)
		}
	})

	t.Run("bare date treated as after", func(t *testing.T) {
		w, err := parseTimeScope("2025-06-01", now)
		if err != nil || w.Min == nil {
			t.Fatalf("expected min set for bare date, got %+v err=%v", w, err)
		}
	})

	t.Run("garbage errors", func(t *testing.T) {
		if _, err := parseTimeScope("whenever-ish", now); err == nil {
			t.Fatal("expected error for unrecognized scope")
		}
	})
}

func TestTruncateRunes(t *testing.T) {
	// Short strings pass through untouched.
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Fatalf("expected passthrough, got %q", got)
	}
	// A string of multibyte runes truncated mid-way must stay valid UTF-8 and not split a rune.
	emoji := strings.Repeat("🙂", 20) // each is 4 bytes, 1 rune
	got := truncateRunes(emoji, 5)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateRunes produced invalid UTF-8: %q", got)
	}
	// 5 kept runes + the ellipsis rune.
	if n := utf8.RuneCountInString(got); n != 6 {
		t.Fatalf("expected 5 runes + ellipsis, got %d runes", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	// Byte-length truncation would have split the 3rd byte of a 4-byte rune; confirm we didn't.
	if strings.ContainsRune(got, '�') {
		t.Fatalf("found replacement char (rune was split): %q", got)
	}
}

// --- cursor round-trip ----------------------------------------------------

func TestCursorRoundTrip(t *testing.T) {
	tok := encodeCursor(recallCursor{Offset: 10, Page: 3})
	got, err := decodeCursor(tok)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got.Offset != 10 || got.Page != 3 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if _, err := decodeCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("expected error for malformed token")
	}
	if c, err := decodeCursor(""); err != nil || c.Offset != 0 {
		t.Fatalf("empty token should decode to zero cursor, got %+v err=%v", c, err)
	}
}

func TestCursorRejectsNegative(t *testing.T) {
	if _, err := decodeCursor(encodeCursor(recallCursor{Offset: -1})); err == nil {
		t.Fatal("expected error for negative offset cursor")
	}
	if _, err := decodeCursor(encodeCursor(recallCursor{Page: -5})); err == nil {
		t.Fatal("expected error for negative page cursor")
	}
}

// --- source_type normalization -------------------------------------------

func TestSourceTypeNormalization(t *testing.T) {
	rt := &RecallTool{}
	cases := map[string]string{
		"":              recallSourceAll,
		"MEMORIES":      recallSourceMemories,
		"files":         recallSourceFiles,
		"conversations": recallSourceConversations,
		"threads":       recallSourceConversations, // legacy alias
		"THREADS":       recallSourceConversations, // legacy alias, case-insensitive
		"bogus":         recallSourceAll,
	}
	for in, want := range cases {
		if got := rt.sourceType(in); got != want {
			t.Fatalf("sourceType(%q)=%q want %q", in, got, want)
		}
	}
}

// --- fake store + mode routing --------------------------------------------

type fakeRecallStore struct {
	relatedMemories  []*models.Memory
	fileChunks       []datastore.FileChunkResult
	chunksForAtt     []datastore.FileChunkResult
	memoryByID       map[uuid.UUID]*models.Memory
	fileByID         map[uuid.UUID]*models.FileAttachment
	fileList         []*models.FileAttachment
	messages         []*models.ChatMessage
	summaryByChatID  map[uuid.UUID]*models.Memory
	relatedSummaries []*models.Memory
	mergeEvents      []*models.MemoryMergeEvent

	// captured inputs for assertions
	lastMsgChatID      uuid.UUID
	lastMsgFilters     models.ChatMessageFilters
	lastMsgPageSize    int
	lastMsgAfterSentAt time.Time
	lastMsgAfterID     uuid.UUID
	lastFileLimit      int
	lastFilePID        *uuid.UUID
	lastSummaryLimit   int
	lastMergeFilters   models.MemoryMergeEventFilters
	lastMergePageNum   int
	lastMergePageSize  int
}

type recallImageStore struct{ content []byte }

func (s recallImageStore) UploadFile(context.Context, string, []byte, string) error { return nil }
func (s recallImageStore) DownloadFile(context.Context, string) ([]byte, error) {
	return s.content, nil
}
func (s recallImageStore) DeleteFile(context.Context, string) error { return nil }

func (f *fakeRecallStore) GetRelatedMemories(_ context.Context, _, _ uuid.UUID, _ []float32, _ uuid.UUID) ([]*models.Memory, error) {
	return f.relatedMemories, nil
}
func (f *fakeRecallStore) GetMemory(_ context.Context, _, id uuid.UUID) (*models.Memory, error) {
	if m, ok := f.memoryByID[id]; ok {
		return m, nil
	}
	return nil, nil
}
func (f *fakeRecallStore) GetMemoryByIDPrefix(_ context.Context, _ uuid.UUID, prefix string) (*models.Memory, error) {
	prefix = strings.ToLower(strings.ReplaceAll(prefix, "-", ""))
	var match *models.Memory
	for id, m := range f.memoryByID {
		compact := strings.ReplaceAll(id.String(), "-", "")
		if strings.HasPrefix(compact, prefix) {
			if match != nil {
				return nil, fmt.Errorf("memory ID prefix %q is ambiguous; pass the full UUID", prefix)
			}
			match = m
		}
	}
	return match, nil
}
func (f *fakeRecallStore) GetRelatedFileChunks(_ context.Context, _ uuid.UUID, personalityID *uuid.UUID, _ *uuid.UUID, _ []float32, limit int) ([]datastore.FileChunkResult, error) {
	f.lastFileLimit = limit
	f.lastFilePID = personalityID
	return f.fileChunks, nil
}
func (f *fakeRecallStore) ListFileChunksForAttachment(_ context.Context, _ uuid.UUID, _ int) ([]datastore.FileChunkResult, error) {
	return f.chunksForAtt, nil
}
func (f *fakeRecallStore) ListFileAttachments(_ context.Context, _ uuid.UUID, _, _ int, _ models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	results := make([]any, 0, len(f.fileList))
	for _, fa := range f.fileList {
		results = append(results, fa)
	}
	return &models.PaginatedResponse{Results: results}, nil
}
func (f *fakeRecallStore) GetFileAttachment(_ context.Context, _, id uuid.UUID) (*models.FileAttachment, error) {
	if fa, ok := f.fileByID[id]; ok {
		return fa, nil
	}
	return nil, nil
}
func (f *fakeRecallStore) GetChatMessage(_ context.Context, _, id uuid.UUID) (*models.ChatMessage, error) {
	for _, m := range f.messages {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, nil
}
func (f *fakeRecallStore) ListChatMessageBookmarksPage(_ context.Context, _, _ uuid.UUID, _, _ int) (*models.PaginatedResponse, error) {
	return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
}
func (f *fakeRecallStore) ListChatMessages(_ context.Context, _, chatID uuid.UUID, pageNum, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	f.lastMsgChatID = chatID
	f.lastMsgFilters = filters
	f.lastMsgPageSize = pageSize
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	total := len(f.messages)
	start := (pageNum - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	results := make([]any, 0, end-start)
	for _, m := range f.messages[start:end] {
		results = append(results, m)
	}
	return &models.PaginatedResponse{Results: results, TotalCount: total, Page: pageNum}, nil
}
func (f *fakeRecallStore) ListChatMessagesAfter(_ context.Context, _, chatID uuid.UUID, afterSentAt time.Time, afterID uuid.UUID, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	f.lastMsgChatID = chatID
	f.lastMsgFilters = filters
	f.lastMsgPageSize = pageSize
	f.lastMsgAfterSentAt = afterSentAt
	f.lastMsgAfterID = afterID

	messages := append([]*models.ChatMessage(nil), f.messages...)
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].SentAt.Equal(messages[j].SentAt) {
			return messages[i].ID.String() < messages[j].ID.String()
		}
		return messages[i].SentAt.Before(messages[j].SentAt)
	})
	eligible := make([]*models.ChatMessage, 0, len(messages))
	for _, m := range messages {
		if filters.MinDate != nil && m.SentAt.Before(*filters.MinDate) {
			continue
		}
		if filters.MaxDate != nil && m.SentAt.After(*filters.MaxDate) {
			continue
		}
		eligible = append(eligible, m)
	}
	total := len(eligible)
	filtered := make([]*models.ChatMessage, 0, len(eligible))
	for _, m := range eligible {
		if !afterSentAt.IsZero() && (m.SentAt.Before(afterSentAt) || (m.SentAt.Equal(afterSentAt) && m.ID.String() <= afterID.String())) {
			continue
		}
		filtered = append(filtered, m)
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if len(filtered) > pageSize {
		filtered = filtered[:pageSize]
	}
	results := make([]any, 0, len(filtered))
	for _, m := range filtered {
		results = append(results, m)
	}
	return &models.PaginatedResponse{Results: results, TotalCount: total, Page: 1}, nil
}
func (f *fakeRecallStore) GetChatSummaryMemory(_ context.Context, _, chatID uuid.UUID) (*models.Memory, error) {
	if m, ok := f.summaryByChatID[chatID]; ok {
		return m, nil
	}
	return nil, nil
}
func (f *fakeRecallStore) GetRelatedSummaryMemories(_ context.Context, _ uuid.UUID, _ []float32, limit int) ([]*models.Memory, error) {
	f.lastSummaryLimit = limit
	return f.relatedSummaries, nil
}
func (f *fakeRecallStore) ListMemoryMergeEvents(_ context.Context, _ uuid.UUID, pageNum, pageSize int, filters models.MemoryMergeEventFilters) (*models.PaginatedResponse, error) {
	f.lastMergePageNum = pageNum
	f.lastMergePageSize = pageSize
	f.lastMergeFilters = filters
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	total := len(f.mergeEvents)
	start := (pageNum - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	results := make([]any, 0, end-start)
	for _, ev := range f.mergeEvents[start:end] {
		results = append(results, ev)
	}
	return &models.PaginatedResponse{Results: results, TotalCount: total, Page: pageNum}, nil
}

func newTestRecallTool(store recallStore) *RecallTool {
	return &RecallTool{
		store:  store,
		embed:  func(_ context.Context, _ string) ([]float32, error) { return []float32{0.1, 0.2}, nil },
		logger: zap.NewNop(),
	}
}

func recallTestChat() *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
}

func decodeRecall(t *testing.T, out string) recallResult {
	t.Helper()
	var res recallResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("failed to decode recall result: %v (%s)", err, out)
	}
	return res
}

func TestRecallUnknownMode(t *testing.T) {
	rt := newTestRecallTool(&fakeRecallStore{})
	out, mems, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"teleport"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mems != nil {
		t.Fatalf("expected no memories, got %d", len(mems))
	}
	if atts != nil {
		t.Fatalf("expected no attachments, got %d", len(atts))
	}
	res := decodeRecall(t, out)
	if res.Error == "" {
		t.Fatal("expected error field set for unknown mode")
	}
}

// Logical failures (here: an unparseable time_scope) must come back as a structured Error payload
// with a nil error, so the agent loop surfaces it to the model instead of flagging a hard failure.
func TestRecallLogicalFailureReturnsNilError(t *testing.T) {
	rt := newTestRecallTool(&fakeRecallStore{})
	out, mems, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"search","query":"x","time_scope":"whenever-ish"}`))
	if err != nil {
		t.Fatalf("expected nil error for a logical failure, got %v", err)
	}
	if mems != nil {
		t.Fatalf("expected no memories on failure, got %d", len(mems))
	}
	if atts != nil {
		t.Fatalf("expected no attachments on failure, got %d", len(atts))
	}
	res := decodeRecall(t, out)
	if res.Error == "" {
		t.Fatal("expected the failure surfaced in the Error field")
	}
}

func TestRecallDefaultsToInvestigate(t *testing.T) {
	rt := newTestRecallTool(&fakeRecallStore{})
	// No mode supplied — should route to investigate and validate its own inputs.
	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"query":"anything"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if res.Mode != recallModeInvestigate {
		t.Fatalf("expected default mode investigate, got %q", res.Mode)
	}
}

func TestRecallSearchReturnsMemoriesAndChunks(t *testing.T) {
	mem := &models.Memory{ID: uuid.New(), Content: "Gori prefers 30k context windows", CreatedAt: time.Now()}
	store := &fakeRecallStore{
		relatedMemories: []*models.Memory{mem},
		fileChunks:      []datastore.FileChunkResult{{FileName: "spec.md", Sequence: 2, Content: "chunk text", CreatedAt: time.Now()}},
	}
	rt := newTestRecallTool(store)

	out, mems, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"search","query":"context windows"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 || mems[0].ID != mem.ID {
		t.Fatalf("expected the seed memory returned for turn-context loading, got %+v", mems)
	}
	res := decodeRecall(t, out)
	if len(res.Memories) != 1 || len(res.Chunks) != 1 {
		t.Fatalf("expected 1 memory + 1 chunk, got %+v", res)
	}
	if res.Chunks[0].Name != "spec.md" || res.Chunks[0].SourceType != "file" {
		t.Fatalf("unexpected chunk shape: %+v", res.Chunks[0])
	}
}

// The regression test for the top finding: time_scope must actually change what search returns.
func TestRecallSearchTimeScopeFiltersMemories(t *testing.T) {
	now := time.Now()
	recent := &models.Memory{ID: uuid.New(), Content: "recent fact", CreatedAt: now.Add(-2 * 24 * time.Hour)}
	old := &models.Memory{ID: uuid.New(), Content: "old fact", CreatedAt: now.Add(-90 * 24 * time.Hour)}
	store := &fakeRecallStore{relatedMemories: []*models.Memory{recent, old}}
	rt := newTestRecallTool(store)
	chat := recallTestChat()

	// Without time_scope: both returned.
	out, _, _, _ := rt.Recall(context.Background(), chat, []byte(`{"mode":"search","query":"fact","source_type":"memories"}`))
	if got := len(decodeRecall(t, out).Memories); got != 2 {
		t.Fatalf("expected 2 memories without time_scope, got %d", got)
	}

	// With "last 7 days": only the recent one survives.
	out, mems, _, _ := rt.Recall(context.Background(), chat, []byte(`{"mode":"search","query":"fact","source_type":"memories","time_scope":"last 7 days"}`))
	res := decodeRecall(t, out)
	if len(res.Memories) != 1 || len(mems) != 1 || mems[0].ID != recent.ID {
		t.Fatalf("expected only the recent memory under last-7-days, got result=%+v mems=%d", res.Memories, len(mems))
	}
}

func TestRecallSearchTimeScopeFiltersFiles(t *testing.T) {
	now := time.Now()
	store := &fakeRecallStore{
		fileChunks: []datastore.FileChunkResult{
			{FileName: "recent.md", Sequence: 0, Content: "new", CreatedAt: now.Add(-1 * 24 * time.Hour)},
			{FileName: "ancient.md", Sequence: 0, Content: "old", CreatedAt: now.Add(-100 * 24 * time.Hour)},
		},
	}
	rt := newTestRecallTool(store)

	out, _, _, _ := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"search","query":"x","source_type":"files","time_scope":"last 7 days"}`))
	res := decodeRecall(t, out)
	if len(res.Chunks) != 1 || res.Chunks[0].Name != "recent.md" {
		t.Fatalf("expected only recent.md under last-7-days, got %+v", res.Chunks)
	}
	// Over-fetch should have widened the store limit beyond max_chunks when time-scoped.
	if store.lastFileLimit <= recallDefaultMaxChunks {
		t.Fatalf("expected over-fetch when time-scoped, got limit=%d", store.lastFileLimit)
	}
}

// source_type=summaries searches per-chat checkpoint Summary memories and returns them as
// "summary" chunks carrying the source conversation_id — a distinct path from the facts-only
// GetRelatedMemories used by source_type=memories.
func TestRecallSearchSummariesReturnsSummaryChunks(t *testing.T) {
	chatID := uuid.New()
	sum := &models.Memory{ID: uuid.New(), ChatID: chatID, ChatName: "Vacation planning", Content: "Discussed itinerary for Portugal trip.", CreatedAt: time.Now()}
	store := &fakeRecallStore{relatedSummaries: []*models.Memory{sum}}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"search","query":"portugal trip","source_type":"summaries"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if len(res.Chunks) != 1 {
		t.Fatalf("expected 1 summary chunk, got %+v", res.Chunks)
	}
	chunk := res.Chunks[0]
	if chunk.SourceType != "summary" || chunk.Name != "Vacation planning" || chunk.ConversationID != chatID.String() {
		t.Fatalf("unexpected summary chunk shape: %+v", chunk)
	}
	if chunk.Text != sum.Content {
		t.Fatalf("expected summary content in chunk text, got %q", chunk.Text)
	}
}

func TestRecallCurrentConversationScopesFilesAndMemories(t *testing.T) {
	chat := recallTestChat()
	now := time.Now()
	chatMem := &models.Memory{ID: uuid.New(), ChatID: chat.ID, Content: "this conversation", CreatedAt: now}
	userMem := &models.Memory{ID: uuid.New(), Content: "global", CreatedAt: now} // ChatID zero
	store := &fakeRecallStore{relatedMemories: []*models.Memory{chatMem, userMem}}
	rt := newTestRecallTool(store)

	out, _, _, _ := rt.Recall(context.Background(), chat, []byte(`{"mode":"search","query":"x","source_type":"memories","current_conversation":true}`))
	res := decodeRecall(t, out)
	if len(res.Memories) != 1 {
		t.Fatalf("expected only the current-conversation memory, got %+v", res.Memories)
	}
	// Files must be scoped to chat-only (nil personality) when current_conversation.
	_, _, _, _ = rt.Recall(context.Background(), chat, []byte(`{"mode":"search","query":"x","source_type":"files","current_conversation":true}`))
	if store.lastFilePID != nil {
		t.Fatalf("expected nil personalityID for current-conversation file scope, got %v", store.lastFilePID)
	}
}

func TestRecallInvestigateNoDistillerFallsBack(t *testing.T) {
	mem := &models.Memory{ID: uuid.New(), Content: "trial bucket is 1000 credits", CreatedAt: time.Now()}
	rt := newTestRecallTool(&fakeRecallStore{relatedMemories: []*models.Memory{mem}})

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"investigate","query":"how big is the trial bucket?"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if res.Answer != "" {
		t.Fatalf("expected no distilled answer without a distiller, got %q", res.Answer)
	}
	if len(res.Memories) == 0 || res.Note == "" {
		t.Fatalf("expected raw material + note in fallback, got %+v", res)
	}
}

func TestRecallFetchMemoryByID(t *testing.T) {
	id := uuid.New()
	mem := &models.Memory{ID: id, Content: "bowling league is on Tuesdays", CreatedAt: time.Now()}
	rt := newTestRecallTool(&fakeRecallStore{memoryByID: map[uuid.UUID]*models.Memory{id: mem}})

	out, mems, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"`+id.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 || mems[0].ID != id {
		t.Fatalf("expected fetched memory loaded into turn context, got %+v", mems)
	}
	if atts != nil {
		t.Fatalf("expected no attachments for a memory fetch, got %d", len(atts))
	}
	res := decodeRecall(t, out)
	if len(res.Memories) != 1 {
		t.Fatalf("expected memory in result, got %+v", res)
	}
	if !strings.HasPrefix(res.Memories[0], "memory:"+id.String()+" ") {
		t.Fatalf("expected memory line prefixed with full memory:<uuid>, got %q", res.Memories[0])
	}
}

func TestRecallFetchByFilenameExactVsFuzzy(t *testing.T) {
	exact := &models.FileAttachment{ID: uuid.New(), Name: "vix_steering_buckets.md"}
	other := &models.FileAttachment{ID: uuid.New(), Name: "notes_about_vix.md"}

	// Fuzzy-only match: the filter surfaces `other`, but its name isn't exact -> suggestion note.
	fuzzy := &fakeRecallStore{fileList: []*models.FileAttachment{other}}
	rt := newTestRecallTool(fuzzy)
	out, _, _, _ := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"vix_steering_buckets.md"}`))
	res := decodeRecall(t, out)
	if len(res.Chunks) != 0 || res.Note == "" {
		t.Fatalf("expected no chunks + a suggestion note on fuzzy-only match, got %+v", res)
	}

	// Exact match present -> resolves and fetches chunks.
	hit := &fakeRecallStore{
		fileList:     []*models.FileAttachment{other, exact},
		chunksForAtt: []datastore.FileChunkResult{{FileName: exact.Name, Sequence: 0, Content: "body"}},
	}
	rt = newTestRecallTool(hit)
	out, _, _, _ = rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"vix_steering_buckets.md"}`))
	res = decodeRecall(t, out)
	if len(res.Chunks) != 1 || res.Chunks[0].Name != exact.Name {
		t.Fatalf("expected exact file chunks, got %+v", res)
	}
}

func TestRecallFetchFileChunksHonorsPageSizeWhenStoreOverReturns(t *testing.T) {
	rows := make([]datastore.FileChunkResult, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, datastore.FileChunkResult{
			FileName: "notes.md",
			Sequence: i,
			Content:  fmt.Sprintf("chunk-%d", i),
		})
	}
	rt := newTestRecallTool(&fakeRecallStore{chunksForAtt: rows})

	out, _, _, err := rt.fetchFileChunks(context.Background(), uuid.New(), "notes.md", "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if len(res.Chunks) != 2 {
		t.Fatalf("expected page size of 2 despite over-returning store, got %+v", res.Chunks)
	}
	if res.Chunks[0].Index != 0 || res.Chunks[1].Index != 1 {
		t.Fatalf("expected first chunk page, got %+v", res.Chunks)
	}
	if !res.HasMore || res.NextPageToken == "" {
		t.Fatalf("expected next page token after capped page, got %+v", res)
	}
}

// Fetching an image attaches it to the model's context (base64 FileContent set on the returned
// attachment) instead of returning text chunks.
func TestRecallFetchImageAttachesToContext(t *testing.T) {
	imgID := uuid.New()
	raw := []byte("fake-png-bytes")
	img := &models.FileAttachment{
		ID:       imgID,
		Name:     "sunset.png",
		FileType: "image/png",
	}
	rt := newTestRecallTool(&fakeRecallStore{fileByID: map[uuid.UUID]*models.FileAttachment{imgID: img}})
	var fileStore storage.FileStore = recallImageStore{content: raw}
	rt.fileStore = fileStore

	out, mems, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"`+imgID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 0 {
		t.Fatalf("expected no memories for an image fetch, got %+v", mems)
	}
	if len(atts) != 1 {
		t.Fatalf("expected exactly one attachment, got %d", len(atts))
	}
	if atts[0].FileContent == "" {
		t.Fatal("expected FileContent to be set on the returned attachment")
	}
	decoded, decErr := base64.StdEncoding.DecodeString(atts[0].FileContent)
	if decErr != nil || string(decoded) != string(raw) {
		t.Fatalf("expected the attachment's FileContent to decode to the source bytes, got %q err=%v", atts[0].FileContent, decErr)
	}
	// The original attachment must not be mutated (fetchImage copies before setting FileContent).
	if img.FileContent != "" {
		t.Fatalf("expected source attachment untouched, got %q", img.FileContent)
	}
	res := decodeRecall(t, out)
	if len(res.Chunks) != 0 {
		t.Fatalf("expected no text chunks for an image fetch, got %+v", res.Chunks)
	}
	if res.Note == "" {
		t.Fatal("expected a note confirming the image was attached")
	}
}

// Non-image files must still return chunks (not attachments), unaffected by the image path.
func TestRecallFetchNonImageReturnsChunksNotAttachments(t *testing.T) {
	faID := uuid.New()
	fa := &models.FileAttachment{ID: faID, Name: "notes.md", FileType: "text/markdown"}
	store := &fakeRecallStore{
		fileByID:     map[uuid.UUID]*models.FileAttachment{faID: fa},
		chunksForAtt: []datastore.FileChunkResult{{FileName: fa.Name, Sequence: 0, Content: "body"}},
	}
	rt := newTestRecallTool(store)

	out, _, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"`+faID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atts != nil {
		t.Fatalf("expected no attachments for a non-image file fetch, got %d", len(atts))
	}
	res := decodeRecall(t, out)
	if len(res.Chunks) != 1 || res.Chunks[0].Name != fa.Name {
		t.Fatalf("expected file chunks, got %+v", res.Chunks)
	}
}

func TestRecallRelatedExcludesSeed(t *testing.T) {
	seedID := uuid.New()
	seed := &models.Memory{ID: seedID, Content: "seed", CreatedAt: time.Now()}
	other := &models.Memory{ID: uuid.New(), Content: "neighbor", CreatedAt: time.Now()}
	store := &fakeRecallStore{
		memoryByID:      map[uuid.UUID]*models.Memory{seedID: seed},
		relatedMemories: []*models.Memory{seed, other}, // GetRelatedMemories includes the seed itself
	}
	rt := newTestRecallTool(store)

	out, mems, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"related","target":"`+seedID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 || mems[0].ID != other.ID {
		t.Fatalf("expected only the neighbor (seed excluded), got %+v", mems)
	}
	res := decodeRecall(t, out)
	if len(res.Memories) != 1 {
		t.Fatalf("expected 1 related memory, got %+v", res)
	}
}

func TestRecallOriginLoadsUpToCreationTime(t *testing.T) {
	created := time.Now().Add(-14 * 24 * time.Hour)
	chatID := uuid.New()
	memID := uuid.New()
	mem := &models.Memory{ID: memID, ChatID: chatID, Content: "formed here", CreatedAt: created}
	store := &fakeRecallStore{
		memoryByID: map[uuid.UUID]*models.Memory{memID: mem},
		messages:   []*models.ChatMessage{{ID: uuid.New(), Message: "hi", Origin: models.MessageOriginUser, SentAt: created.Add(-time.Hour)}},
	}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"origin","target":"`+memID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastMsgChatID != chatID {
		t.Fatalf("expected origin to query the source chat %s, got %s", chatID, store.lastMsgChatID)
	}
	if store.lastMsgFilters.MaxDate == nil || !store.lastMsgFilters.MaxDate.Equal(created) {
		t.Fatalf("expected MaxDate == memory CreatedAt, got %+v", store.lastMsgFilters.MaxDate)
	}
	if store.lastMsgPageSize != recallDefaultOriginMsgs {
		t.Fatalf("expected default origin page size %d, got %d", recallDefaultOriginMsgs, store.lastMsgPageSize)
	}
	res := decodeRecall(t, out)
	if len(res.Conversations) != 1 || len(res.Conversations[0].Messages) != 1 {
		t.Fatalf("expected one conversation with the source message, got %+v", res.Conversations)
	}
	if !strings.Contains(res.Note, "thin slice") {
		t.Fatalf("expected origin window note on page 1, got %q", res.Note)
	}
}

func TestRecallOriginPaginatesEarlierTurns(t *testing.T) {
	created := time.Now()
	chatID := uuid.New()
	memID := uuid.New()
	mem := &models.Memory{ID: memID, ChatID: chatID, Content: "formed here", CreatedAt: created}
	// More than one page → has_more / next_page_token.
	msgs := make([]*models.ChatMessage, 0, recallDefaultOriginMsgs+2)
	for i := 0; i < recallDefaultOriginMsgs+2; i++ {
		msgs = append(msgs, &models.ChatMessage{
			ID: uuid.New(), Message: "m", Origin: models.MessageOriginUser,
			SentAt: created.Add(-time.Duration(i+1) * time.Minute),
		})
	}
	store := &fakeRecallStore{memoryByID: map[uuid.UUID]*models.Memory{memID: mem}, messages: msgs}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"origin","target":"`+memID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if !res.HasMore || res.NextPageToken == "" {
		t.Fatalf("expected has_more + next_page_token on page 1, got %+v", res)
	}
	if res.Page != 1 || res.TotalCount != len(msgs) {
		t.Fatalf("expected page=1 total=%d, got page=%d total=%d", len(msgs), res.Page, res.TotalCount)
	}
	if !strings.Contains(res.Note, "next_page_token") {
		t.Fatalf("expected note pointing at next_page_token, got %q", res.Note)
	}
	page1Token := res.NextPageToken

	out2, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"origin","target":"`+memID.String()+`","next_page_token":"`+page1Token+`"}`))
	if err != nil {
		t.Fatalf("page 2 unexpected error: %v", err)
	}
	res2 := decodeRecall(t, out2)
	if strings.Contains(res2.Note, "thin slice") {
		t.Fatalf("window note should only appear on page 1, got %q", res2.Note)
	}
	if res2.Page != 2 {
		t.Fatalf("expected page=2, got %d", res2.Page)
	}
	if res2.NextPageToken == page1Token {
		t.Fatalf("next_page_token must advance (or clear); still %q", res2.NextPageToken)
	}
	if res2.HasMore {
		t.Fatalf("expected no further pages, got has_more with token %q", res2.NextPageToken)
	}
}

func TestRecallConversationDefaultsToCurrentAndAppliesWindow(t *testing.T) {
	chat := recallTestChat()
	store := &fakeRecallStore{
		messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now().Add(-time.Hour)}},
	}
	rt := newTestRecallTool(store)

	// No target -> defaults to the current conversation; time_scope applies a SentAt window.
	out, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation","time_scope":"last 7 days"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastMsgChatID != chat.ID {
		t.Fatalf("expected conversation mode to default to current chat %s, got %s", chat.ID, store.lastMsgChatID)
	}
	if store.lastMsgFilters.MinDate == nil {
		t.Fatalf("expected a SentAt lower bound from time_scope, got nil")
	}
	res := decodeRecall(t, out)
	if len(res.Conversations) != 1 {
		t.Fatalf("expected exactly one conversation, got %d", len(res.Conversations))
	}
	if res.Mode != recallModeConversation {
		t.Fatalf("expected mode=%q in result, got %q", recallModeConversation, res.Mode)
	}
}

func TestRecallConversationPaginatesChronologicallyWithKeysetCursor(t *testing.T) {
	chat := recallTestChat()
	start := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	msgs := []*models.ChatMessage{
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000005"), Message: "fifth", Origin: models.MessageOriginAssistant, SentAt: start.Add(4 * time.Minute)},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Message: "first", Origin: models.MessageOriginUser, SentAt: start},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), Message: "third", Origin: models.MessageOriginUser, SentAt: start.Add(2 * time.Minute)},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Message: "second", Origin: models.MessageOriginAssistant, SentAt: start.Add(time.Minute)},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000004"), Message: "fourth", Origin: models.MessageOriginAssistant, SentAt: start.Add(3 * time.Minute)},
	}
	store := &fakeRecallStore{messages: msgs}
	rt := newTestRecallTool(store)

	out1, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation","max_chunks":2}`))
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	page1 := decodeRecall(t, out1)
	if got := []string{page1.Conversations[0].Messages[0].Text, page1.Conversations[0].Messages[1].Text}; strings.Join(got, ",") != "first,second" {
		t.Fatalf("first page must start at conversation beginning, got %v", got)
	}
	if !page1.HasMore || page1.NextPageToken == "" {
		t.Fatalf("expected keyset continuation token, got %+v", page1)
	}
	cur, err := decodeCursor(page1.NextPageToken)
	if err != nil || cur.AfterMessageID != msgs[3].ID.String() || cur.ConversationID != chat.ID.String() || cur.PageSize != 2 {
		t.Fatalf("expected cursor anchored to second message and conversation, got %+v err=%v", cur, err)
	}

	out2, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation","max_chunks":2,"next_page_token":"`+page1.NextPageToken+`"}`))
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	page2 := decodeRecall(t, out2)
	if got := []string{page2.Conversations[0].Messages[0].Text, page2.Conversations[0].Messages[1].Text}; strings.Join(got, ",") != "third,fourth" {
		t.Fatalf("second page must continue forward, got %v", got)
	}
	if !store.lastMsgAfterSentAt.Equal(start.Add(time.Minute)) || store.lastMsgAfterID != msgs[3].ID {
		t.Fatalf("second request did not use first page's keyset cursor: %v / %s", store.lastMsgAfterSentAt, store.lastMsgAfterID)
	}

	out3, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation","max_chunks":2,"next_page_token":"`+page2.NextPageToken+`"}`))
	if err != nil {
		t.Fatalf("third page: %v", err)
	}
	page3 := decodeRecall(t, out3)
	if got := []string{page3.Conversations[0].Messages[0].Text}; strings.Join(got, ",") != "fifth" || page3.HasMore || page3.NextPageToken != "" {
		t.Fatalf("expected final chronological page without continuation, got %+v", page3)
	}
}

func TestRecallConversationRejectsCursorForDifferentConversation(t *testing.T) {
	chat := recallTestChat()
	token := encodeCursor(recallCursor{
		Page:           2,
		AfterSentAt:    time.Now().UTC().Format(time.RFC3339Nano),
		AfterMessageID: uuid.New().String(),
		ConversationID: uuid.New().String(),
	})
	out, _, _, err := newTestRecallTool(&fakeRecallStore{}).Recall(context.Background(), chat, []byte(`{"mode":"conversation","next_page_token":"`+token+`"}`))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if decodeRecall(t, out).Error == "" {
		t.Fatal("expected mismatched conversation cursor to fail")
	}
}

func TestMessagesFromPagePreservesSubsecondChronology(t *testing.T) {
	sentAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	early := &models.ChatMessage{
		ID:      uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Message: "early",
		SentAt:  sentAt.Add(100 * time.Millisecond),
	}
	late := &models.ChatMessage{
		ID:      uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Message: "late",
		SentAt:  sentAt.Add(900 * time.Millisecond),
	}
	page := &models.PaginatedResponse{Results: []any{late, early}}

	got := messagesFromPage(page)
	if len(got) != 2 || got[0].Text != "early" || got[1].Text != "late" {
		t.Fatalf("expected precise chronological ordering, got %+v", got)
	}
}

// mode "thread" is a legacy alias for "conversation": it must still work and the result must
// report the canonical mode name.
func TestRecallModeThreadAliasWorksAndEmitsConversationMode(t *testing.T) {
	chat := recallTestChat()
	store := &fakeRecallStore{
		messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now().Add(-time.Hour)}},
	}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"thread"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastMsgChatID != chat.ID {
		t.Fatalf("expected the thread alias to default to the current chat %s, got %s", chat.ID, store.lastMsgChatID)
	}
	res := decodeRecall(t, out)
	if res.Mode != recallModeConversation {
		t.Fatalf("expected mode=%q from the thread alias, got %q", recallModeConversation, res.Mode)
	}
	if len(res.Conversations) != 1 {
		t.Fatalf("expected exactly one conversation, got %d", len(res.Conversations))
	}
}

func TestRecallConversationInvalidTarget(t *testing.T) {
	rt := newTestRecallTool(&fakeRecallStore{})
	out, _, _, _ := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"conversation","target":"not-a-uuid"}`))
	if decodeRecall(t, out).Error == "" {
		t.Fatal("expected error for invalid conversation target")
	}
}

// target="current_conversation" (case-insensitive) resolves to the current chat, same as an
// empty target.
func TestRecallConversationTargetCurrentConversationSentinel(t *testing.T) {
	chat := recallTestChat()
	store := &fakeRecallStore{
		messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hi", Origin: models.MessageOriginUser, SentAt: time.Now()}},
	}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation","target":"CURRENT_CONVERSATION"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastMsgChatID != chat.ID {
		t.Fatalf("expected the current_conversation sentinel to resolve to chat %s, got %s", chat.ID, store.lastMsgChatID)
	}
	res := decodeRecall(t, out)
	if len(res.Conversations) != 1 || res.Conversations[0].ConversationID != chat.ID.String() {
		t.Fatalf("expected the current conversation returned, got %+v", res.Conversations)
	}
}

// The positive distillation path: when a distiller succeeds, investigate returns the compressed
// Answer + Sources and must NOT leak the raw material (chunks/memories) into the result.
func TestRecallInvestigateWithDistiller(t *testing.T) {
	mem := &models.Memory{ID: uuid.New(), Content: "trial bucket is 1000 credits", CreatedAt: time.Now()}
	store := &fakeRecallStore{
		relatedMemories: []*models.Memory{mem},
		fileChunks:      []datastore.FileChunkResult{{FileName: "pricing.md", Sequence: 0, Content: "1000-credit trial bucket", CreatedAt: time.Now()}},
	}
	dist := &fakeDistiller{answer: "The trial bucket holds 1000 credits. [1]"}
	rt := newTestRecallTool(store)
	rt.distiller = dist

	out, mems, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"investigate","query":"how big is the trial bucket?"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)

	// Distilled answer is wired through verbatim.
	if res.Answer != dist.answer {
		t.Fatalf("expected distilled answer %q, got %q", dist.answer, res.Answer)
	}
	// Sources are surfaced (one per retrieved memory + chunk).
	if len(res.Sources) != 2 {
		t.Fatalf("expected 2 source labels (memory + file), got %+v", res.Sources)
	}
	// Raw material must NOT leak into the result on the success path.
	if len(res.Chunks) != 0 || len(res.Memories) != 0 {
		t.Fatalf("expected no raw chunks/memories in distilled result, got chunks=%d memories=%d", len(res.Chunks), len(res.Memories))
	}
	// Memories are still returned to the caller for turn-context loading.
	if len(mems) != 1 || mems[0].ID != mem.ID {
		t.Fatalf("expected the seed memory returned for turn-context loading, got %+v", mems)
	}
	// The distiller actually received the question and the assembled material.
	if dist.calls != 1 {
		t.Fatalf("expected distiller called once, got %d", dist.calls)
	}
	if dist.gotQuestion == "" || !strings.Contains(dist.gotMaterial, "trial bucket is 1000 credits") {
		t.Fatalf("distiller did not receive expected inputs: question=%q material=%q", dist.gotQuestion, dist.gotMaterial)
	}
}

// When the distiller errors, investigate degrades to returning the raw material with a note rather
// than failing the tool call.
func TestRecallInvestigateDistillerErrorFallsBack(t *testing.T) {
	mem := &models.Memory{ID: uuid.New(), Content: "some fact", CreatedAt: time.Now()}
	dist := &fakeDistiller{err: errors.New("model unavailable")}
	rt := newTestRecallTool(&fakeRecallStore{relatedMemories: []*models.Memory{mem}})
	rt.distiller = dist

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"investigate","query":"what do i know?"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if res.Answer != "" {
		t.Fatalf("expected no answer when distillation fails, got %q", res.Answer)
	}
	if len(res.Memories) == 0 || res.Note == "" {
		t.Fatalf("expected raw material + note on distiller failure, got %+v", res)
	}
}

func TestRecallRelatedAcceptsMemoryPrefixedAndShortIDs(t *testing.T) {
	seedID := uuid.MustParse("df3e519d-1234-5678-9abc-def012345678")
	seed := &models.Memory{ID: seedID, Content: "seed fact", CreatedAt: time.Now()}
	neighbor := &models.Memory{ID: uuid.New(), Content: "neighbor", CreatedAt: time.Now()}
	store := &fakeRecallStore{
		memoryByID:      map[uuid.UUID]*models.Memory{seedID: seed},
		relatedMemories: []*models.Memory{seed, neighbor},
	}
	rt := newTestRecallTool(store)

	for _, target := range []string{
		seedID.String(),
		"memory:" + seedID.String(),
		"memory:df3e519d", // short prefix from historical investigate.sources
	} {
		out, mems, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"related","target":"`+target+`"}`))
		if err != nil {
			t.Fatalf("target %q: unexpected error: %v", target, err)
		}
		res := decodeRecall(t, out)
		if res.Error != "" {
			t.Fatalf("target %q: expected success, got error %q", target, res.Error)
		}
		if len(mems) != 1 || mems[0].ID != neighbor.ID {
			t.Fatalf("target %q: expected neighbor only, got %+v", target, mems)
		}
		if res.SearchType != "semantic" {
			t.Fatalf("target %q: expected search_type=semantic, got %q", target, res.SearchType)
		}
		if !strings.Contains(res.Note, "semantic similarity") {
			t.Fatalf("target %q: expected semantic note, got %q", target, res.Note)
		}
	}
}

func TestRecallInvestigateSourcesUseFullMemoryUUID(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	mem := &models.Memory{ID: id, Content: "full id source", CreatedAt: time.Now()}
	rt := newTestRecallTool(&fakeRecallStore{relatedMemories: []*models.Memory{mem}})
	// No distiller → sources still emitted on the fallback path.
	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"investigate","query":"what?"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	want := "memory:" + id.String()
	if len(res.Sources) != 1 || res.Sources[0] != want {
		t.Fatalf("expected full-uuid source %q, got %+v", want, res.Sources)
	}
	if !strings.HasPrefix(res.Memories[0], want+" ") {
		t.Fatalf("expected memory line prefixed with %q, got %q", want, res.Memories[0])
	}
}

func TestRecallSearchSemanticNoteAndTimeScopeReceipt(t *testing.T) {
	mem := &models.Memory{ID: uuid.New(), Content: "recent", CreatedAt: time.Now()}
	rt := newTestRecallTool(&fakeRecallStore{relatedMemories: []*models.Memory{mem}})
	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"search","query":"x","source_type":"memories","time_scope":"last 7 days"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if res.SearchType != "semantic" {
		t.Fatalf("expected search_type=semantic, got %q", res.SearchType)
	}
	if !strings.Contains(res.Note, "semantic similarity") {
		t.Fatalf("expected semantic note, got %q", res.Note)
	}
	if res.TimeScope == nil || res.TimeScope.Input != "last 7 days" {
		t.Fatalf("expected time_scope receipt for input, got %+v", res.TimeScope)
	}
	if res.TimeScope.NormalizedStart == "" || res.TimeScope.NormalizedEnd == "" {
		t.Fatalf("expected normalized start/end on last-N receipt, got %+v", res.TimeScope)
	}
	if res.TimeScope.Timezone != "UTC" {
		t.Fatalf("expected timezone UTC, got %q", res.TimeScope.Timezone)
	}
	start, err1 := time.Parse(time.RFC3339, res.TimeScope.NormalizedStart)
	end, err2 := time.Parse(time.RFC3339, res.TimeScope.NormalizedEnd)
	if err1 != nil || err2 != nil {
		t.Fatalf("could not parse receipt bounds: %v %v", err1, err2)
	}
	if !end.After(start) {
		t.Fatalf("expected end after start, got start=%v end=%v", start, end)
	}
}

// fetch by chat UUID falls back to the conversation's checkpoint summary when it's neither a
// memory nor a file attachment ID.
func TestRecallFetchByChatUUIDReturnsSummary(t *testing.T) {
	chatID := uuid.New()
	sum := &models.Memory{ID: uuid.New(), ChatID: chatID, ChatName: "Portugal trip", Content: "Checkpoint summary content."}
	store := &fakeRecallStore{summaryByChatID: map[uuid.UUID]*models.Memory{chatID: sum}}
	rt := newTestRecallTool(store)

	out, mems, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"`+chatID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atts != nil {
		t.Fatalf("expected no attachments for a summary fetch, got %d", len(atts))
	}
	if len(mems) != 1 || mems[0].ID != sum.ID {
		t.Fatalf("expected the summary memory loaded into turn context, got %+v", mems)
	}
	res := decodeRecall(t, out)
	if len(res.Chunks) != 1 {
		t.Fatalf("expected 1 summary chunk, got %+v", res)
	}
	chunk := res.Chunks[0]
	if chunk.SourceType != "summary" || chunk.ConversationID != chatID.String() || chunk.Text != sum.Content {
		t.Fatalf("unexpected summary chunk shape: %+v", chunk)
	}
}

// The summary:<conversation-id> target form resolves the same way as a bare chat UUID.
func TestRecallFetchSummaryPrefixedTarget(t *testing.T) {
	chatID := uuid.New()
	sum := &models.Memory{ID: uuid.New(), ChatID: chatID, ChatName: "Portugal trip", Content: "Checkpoint summary content."}
	store := &fakeRecallStore{summaryByChatID: map[uuid.UUID]*models.Memory{chatID: sum}}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"fetch","target":"summary:`+chatID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if len(res.Chunks) != 1 || res.Chunks[0].ConversationID != chatID.String() {
		t.Fatalf("expected summary chunk for prefixed target, got %+v", res)
	}
}

// conversation mode attaches the conversation's checkpoint summary (when one exists) alongside
// its messages.
func TestRecallConversationIncludesSummary(t *testing.T) {
	chat := recallTestChat()
	sum := &models.Memory{ID: uuid.New(), ChatID: chat.ID, Content: "Earlier in this chat, we planned a trip."}
	store := &fakeRecallStore{
		messages:        []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now().Add(-time.Hour)}},
		summaryByChatID: map[uuid.UUID]*models.Memory{chat.ID: sum},
	}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if len(res.Conversations) != 1 {
		t.Fatalf("expected exactly one conversation, got %+v", res.Conversations)
	}
	if res.Conversations[0].Summary != sum.Content {
		t.Fatalf("expected conversation summary attached, got %q", res.Conversations[0].Summary)
	}
}

// conversation mode must not fail or fabricate a summary when none exists for the conversation.
func TestRecallConversationWithoutSummaryOmitsField(t *testing.T) {
	chat := recallTestChat()
	store := &fakeRecallStore{
		messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now()}},
	}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), chat, []byte(`{"mode":"conversation"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeRecall(t, out)
	if len(res.Conversations) != 1 || res.Conversations[0].Summary != "" {
		t.Fatalf("expected no summary when none exists, got %+v", res.Conversations)
	}
}

// --- lifecycle_events mode -------------------------------------------------

// lifecycle_events returns events from the store, mapped into the agent-facing shape
// (Snapshot dropped, source members previewed). merge_history is accepted as an input alias.
func TestRecallLifecycleEventsReturnsEvents(t *testing.T) {
	survivorID := uuid.New()
	linkGroupID := uuid.New()
	ev := &models.MemoryMergeEvent{
		ID:               uuid.New(),
		SurvivorMemoryID: survivorID,
		MergeType:        models.MemoryMergeTypeFoldLive,
		Content:          "Prefers dark mode",
		DuplicatesFolded: 2,
		LinkGroupID:      &linkGroupID,
		SourceMembers:    []models.MemoryMergeSourceMember{{Content: "Prefers dark mode", IsNew: true}},
		CreatedAt:        time.Now(),
		Snapshot:         &models.MemoryMergeUndoSnapshot{PriorConfidence: 0.5},
	}
	store := &fakeRecallStore{mergeEvents: []*models.MemoryMergeEvent{ev}}
	rt := newTestRecallTool(store)

	out, mems, atts, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"lifecycle_events"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mems != nil || atts != nil {
		t.Fatalf("expected no memories/attachments from lifecycle_events (audit list, not memory load), got mems=%v atts=%v", mems, atts)
	}
	res := decodeRecall(t, out)
	if len(res.LifecycleEvents) != 1 {
		t.Fatalf("expected 1 lifecycle event, got %+v", res.LifecycleEvents)
	}
	got := res.LifecycleEvents[0]
	if got.ID != ev.ID.String() || got.SurvivorMemoryID != survivorID.String() || got.MergeType != string(models.MemoryMergeTypeFoldLive) {
		t.Fatalf("unexpected lifecycle event shape: %+v", got)
	}
	if got.LinkGroupID != linkGroupID.String() {
		t.Fatalf("expected link_group_id surfaced, got %q", got.LinkGroupID)
	}
	if len(got.SourceMembers) != 1 || got.SourceMembers[0] != "Prefers dark mode" {
		t.Fatalf("expected source member preview, got %+v", got.SourceMembers)
	}
	if !store.lastMergeFilters.ExcludeReverted {
		t.Fatal("expected ExcludeReverted=true by default")
	}
	if res.Page != 1 || res.TotalCount != 1 || res.HasMore {
		t.Fatalf("expected page=1 total=1 has_more=false, got page=%d total=%d has_more=%v", res.Page, res.TotalCount, res.HasMore)
	}
	// Snapshot must never reach the agent payload.
	if strings.Contains(out, "PriorConfidence") || strings.Contains(out, "prior_confidence") {
		t.Fatalf("expected Snapshot omitted from agent payload, got %q", out)
	}

	// Alias still routes to the same mode.
	out2, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"merge_history"}`))
	if err != nil {
		t.Fatalf("alias unexpected error: %v", err)
	}
	if res2 := decodeRecall(t, out2); res2.Mode != recallModeLifecycleEvents {
		t.Fatalf("alias should normalize to lifecycle_events, got %q", res2.Mode)
	}
}

// A target resolves to a survivor memory ID filter (exercising resolveMemory's UUID path).
func TestRecallLifecycleEventsFiltersBySurvivorTarget(t *testing.T) {
	survivorID := uuid.New()
	survivor := &models.Memory{ID: survivorID, Content: "Prefers dark mode"}
	store := &fakeRecallStore{memoryByID: map[uuid.UUID]*models.Memory{survivorID: survivor}}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"lifecycle_events","target":"`+survivorID.String()+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastMergeFilters.SurvivorMemoryID == nil || *store.lastMergeFilters.SurvivorMemoryID != survivorID {
		t.Fatalf("expected SurvivorMemoryID filter set to target, got %+v", store.lastMergeFilters.SurvivorMemoryID)
	}
	if res := decodeRecall(t, out); res.Error != "" {
		t.Fatalf("expected success, got error %q", res.Error)
	}
}

// Pagination envelope advances next_page_token and never re-emits the request cursor.
func TestRecallLifecycleEventsPaginationAdvances(t *testing.T) {
	events := make([]*models.MemoryMergeEvent, 0, 5)
	for i := 0; i < 5; i++ {
		events = append(events, &models.MemoryMergeEvent{
			ID:               uuid.New(),
			SurvivorMemoryID: uuid.New(),
			MergeType:        models.MemoryMergeTypeFoldLive,
			Content:          fmt.Sprintf("event-%d", i),
			CreatedAt:        time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	store := &fakeRecallStore{mergeEvents: events}
	rt := newTestRecallTool(store)

	out, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"lifecycle_events","max_chunks":2}`))
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	res := decodeRecall(t, out)
	if res.Page != 1 || res.TotalCount != 5 || !res.HasMore || res.NextPageToken == "" {
		t.Fatalf("page 1 envelope: %+v", res)
	}
	if len(res.LifecycleEvents) != 2 {
		t.Fatalf("expected 2 events on page 1, got %d", len(res.LifecycleEvents))
	}
	tok1 := res.NextPageToken

	out2, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"lifecycle_events","max_chunks":2,"next_page_token":"`+tok1+`"}`))
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	res2 := decodeRecall(t, out2)
	if res2.Page != 2 || store.lastMergePageNum != 2 {
		t.Fatalf("expected page 2 (store page=%d), got result page=%d", store.lastMergePageNum, res2.Page)
	}
	if res2.NextPageToken == "" || res2.NextPageToken == tok1 {
		t.Fatalf("expected advanced next_page_token, got %q (was %q)", res2.NextPageToken, tok1)
	}
	if !res2.HasMore || res2.TotalCount != 5 {
		t.Fatalf("page 2 should still have more: %+v", res2)
	}
	tok2 := res2.NextPageToken

	out3, _, _, err := rt.Recall(context.Background(), recallTestChat(), []byte(`{"mode":"lifecycle_events","max_chunks":2,"next_page_token":"`+tok2+`"}`))
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	res3 := decodeRecall(t, out3)
	if res3.Page != 3 || res3.HasMore || res3.NextPageToken != "" {
		t.Fatalf("expected final page with no next token: %+v", res3)
	}
	if len(res3.LifecycleEvents) != 1 {
		t.Fatalf("expected 1 event on last page, got %d", len(res3.LifecycleEvents))
	}
}

func TestTimeScopeReceiptLastN(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 45, 49, 0, time.UTC)
	w, err := parseTimeScope("last 7 days", now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := timeScopeReceipt("last 7 days", w, now)
	if r == nil || r.NormalizedStart != now.Add(-7*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("unexpected start: %+v", r)
	}
	if r.NormalizedEnd != now.Format(time.RFC3339) {
		t.Fatalf("unexpected end: %+v", r)
	}
}

func TestParseBookmarkFetchTargetAndBuilder(t *testing.T) {
	chatID := uuid.New()
	messageID := uuid.New()
	target := bookmarkFetchTarget(chatID, messageID)

	gotChatID, gotMessageID, err := parseBookmarkFetchTarget(target)
	require.NoError(t, err)
	require.Equal(t, chatID, gotChatID)
	require.Equal(t, messageID, gotMessageID)

	_, _, err = parseBookmarkFetchTarget("bookmark:not-a-uuid")
	require.Error(t, err)
}

func TestConversationWindow_UsesCursorBounds(t *testing.T) {
	now := time.Now().UTC()
	min := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	max := now.Add(-1 * time.Hour).Format(time.RFC3339Nano)

	window, err := conversationWindow("last 7 days", recallCursor{MinSentAt: min, MaxSentAt: max}, now)
	require.NoError(t, err)
	require.NotNil(t, window.Min)
	require.NotNil(t, window.Max)
	require.Equal(t, min, window.Min.UTC().Format(time.RFC3339Nano))
	require.Equal(t, max, window.Max.UTC().Format(time.RFC3339Nano))
}
