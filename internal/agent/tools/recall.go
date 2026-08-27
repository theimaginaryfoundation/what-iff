package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Recall modes. `investigate` is the default when mode is omitted.
//
// recallModeThreadAlias is the pre-standardization name for recallModeConversation, accepted
// as an input alias for a soft cutover; Recall always normalizes it to recallModeConversation
// before dispatch and results always report mode="conversation".
const (
	recallModeInvestigate  = "investigate"
	recallModeSearch       = "search"
	recallModeFetch        = "fetch"
	recallModeRelated      = "related"
	recallModeOrigin       = "origin"
	recallModeConversation = "conversation"
	// recallModeBookmarks lists user-pinned navigation markers for one conversation.
	recallModeBookmarks   = "bookmarks"
	recallModeThreadAlias = "thread"
	// recallModeLifecycleEvents lists memory fold/link (and related) audit rows — a paginated
	// inspectability surface, not semantic search.
	recallModeLifecycleEvents = "lifecycle_events"
	// recallModeMergeHistoryAlias is the pre-rename name for recallModeLifecycleEvents.
	recallModeMergeHistoryAlias = "merge_history"
)

// Recall source types (the stores a search/investigate spans).
//
// recallSourceThreadsAlias is the pre-standardization name for recallSourceConversations,
// accepted as an input alias for a soft cutover.
const (
	recallSourceAll           = "all"
	recallSourceMemories      = "memories"
	recallSourceFiles         = "files"
	recallSourceConversations = "conversations"
	recallSourceThreadsAlias  = "threads"
	// recallSourceSummaries searches per-chat checkpoint Summary memories — a compressed first-hop
	// over conversations, distinct from recallSourceConversations' current-conversation keyword scan.
	recallSourceSummaries = "summaries"
)

// recallSummaryTargetPrefix resolves a fetch target of the form "summary:<conversation-uuid>" to
// that conversation's checkpoint summary, mirroring the "memory:" prefix convention.
const recallSummaryTargetPrefix = "summary:"

// recallBookmarkTargetPrefix resolves an explicitly pinned message into its full raw text.
const recallBookmarkTargetPrefix = "bookmark:"

// recallTargetCurrentConversation is the sentinel `target` value conversation mode resolves to
// the current chat, alongside the default of an empty target. Case-insensitive.
const recallTargetCurrentConversation = "current_conversation"

const (
	recallDefaultMaxChunks         = 5
	recallDefaultConversationMsgs  = 20
	recallDefaultOriginMsgs        = 5 // thin slice ending at memory.CreatedAt; page for more
	recallDefaultLifecyclePageSize = 20
	findContextAbsoluteMaxChunks   = 32
	recallDistillMaxSources        = 16
	recallDistillMaxSourceLen      = 1200
	recallLifecyclePreviewLen      = 160 // source_members content preview cap
	recallToolName                 = "find_context"
)

const recallOriginWindowNote = "Showing up to 5 turns ending at the memory's creation time — a thin slice vs a full pre-compaction window. Pass next_page_token (or raise max_chunks) for more of the source conversation."

// RecallDescription is the agent-facing tool description. It intentionally leads with
// investigate-as-default and gives one crisp line per mode so the model can route without
// re-reading the schema each time.
const RecallDescription = `Search and retrieve from your unified context store — memories, uploaded files, and past conversations — through one tool with several access modes.

Default to mode "investigate": ask a question and get back a short, sourced answer distilled from the archive. This is cheaper on your context window than reading raw chunks and is the right call for "what do I know about X?".

Modes:
- investigate (default): question -> compressed, sourced answer. Sources use memory:<full-uuid> hop targets. Use to understand something.
- search: query -> raw chunks ranked by semantic similarity (not exact-match). Memory lines are prefixed with memory:<uuid> for follow-up hops. Set source_type=summaries to search past conversations' checkpoint summaries as a cheap first hop before pulling a full conversation.
- fetch: filename or memory/file ID -> that item's chunks (paginated). A conversation ID (or summary:<conversation-id>) fetches that conversation's checkpoint summary instead. Fetching an image attaches it directly to your context instead of returning chunks. Use when you know what you want; don't semantic-search for a file you can name.
- related: memory ID -> similar memories. Accepts a full UUID, memory:<uuid>, or a unique short hex prefix from sources (e.g. memory:df3e519d).
- origin: Have a memory ID and want where it came from? Pass it as target — returns that memory plus the last ~5 turns of its source conversation ending at the memory's creation time. Same ID shapes as related. Page with next_page_token for progressively older turns; forward paging is not available.
- conversation (alias: thread): one conversation's history from its beginning forward -> pass a conversation ID, or omit it (or pass "current_conversation") for the current conversation; filter with time_scope. Also returns that conversation's checkpoint summary, if one exists. Follow next_page_token to continue toward the end. Use to recover what was said, and when.
- bookmarks: pinned message index for one conversation (target or current), oldest first. Returns small labels and fetch targets only; use its bookmark:<conversation-id>:<message-id> target to read one pinned message's full text.
- lifecycle_events (alias: merge_history): paginated audit list of memory lifecycle events (fold/link and related merge-audit rows — not semantic search). Filter with query (content substring), target (a survivor memory ID), and time_scope. Responses include page, total_count, has_more, and next_page_token when more pages exist.

Modifiers: time_scope ("last 7 days", "before 2025-01-01", "between A and B"), source_type (memories|files|conversations|summaries|all), current_conversation (scopes search/investigate to this conversation), next_page_token (page large results — fetch, conversation, origin, and lifecycle_events), max_chunks (depth; origin defaults to 5 turns; lifecycle_events defaults to 20 events per page).

Don't use find_context for information already in your context window, or for general world knowledge.`

// RecallToolSpec is the shared function-tool shape for the unified find_context tool.
// The schema is co-located with the implementation (rather than in toolconstants.go) since
// find_context is a self-contained feature; see ADR 0x017.
var RecallToolSpec = FunctionToolSpec{
	Name:        recallToolName,
	Description: RecallDescription,
	Properties: map[string]interface{}{
		"mode": map[string]interface{}{
			"type":        "string",
			"enum":        []string{recallModeInvestigate, recallModeSearch, recallModeFetch, recallModeRelated, recallModeOrigin, recallModeConversation, recallModeBookmarks, recallModeThreadAlias, recallModeLifecycleEvents, recallModeMergeHistoryAlias},
			"description": "Access mode. Optional — defaults to 'investigate' when omitted. investigate: ask a question, get a compressed sourced answer. search: semantic search returning raw chunks. fetch: retrieve a specific file, memory, conversation summary, or a bookmark fetch target returned by bookmarks mode (images attach directly to your context instead of returning chunks). related: memories similar to a given memory ID. origin: memory ID -> last ~5 source-conversation turns ending at that memory's creation time (page for more). conversation: one conversation's history from its beginning forward (target or current) by time range, including its checkpoint summary. bookmarks: compact pinned-message index for a conversation; use a returned fetch_target for the selected message. 'thread' is accepted as a legacy alias for 'conversation'. lifecycle_events: paginated audit list of memory lifecycle events (fold/link); 'merge_history' is accepted as a legacy alias.",
		},
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Search query (search mode) or question (investigate mode). Natural language.",
		},
		"target": map[string]interface{}{
			"type":        "string",
			"description": "Filename, memory ID, or conversation ID depending on mode. fetch: filename, memory/file ID, a conversation ID (returns its checkpoint summary), summary:<conversation-id>, or bookmark:<conversation-id>:<message-id>. related/origin: memory UUID, memory:<uuid>, or unique short hex prefix from investigate.sources. conversation/bookmarks: conversation ID, or \"current_conversation\" (or omit) for the current conversation. lifecycle_events: a survivor memory ID to filter to that memory's lifecycle events.",
		},
		"time_scope": map[string]interface{}{
			"type":        "string",
			"description": "Time filter, natural language or ISO-8601. Examples: 'last 7 days', 'before 2025-01-01', 'after 2025-06-01', 'between 2025-01-01 and 2025-03-01'. Applies to search/investigate (memories and summaries by their created date, files by upload date), conversation (messages by sent time), and lifecycle_events (event created date).",
		},
		"source_type": map[string]interface{}{
			"type":        "string",
			"enum":        []string{recallSourceAll, recallSourceMemories, recallSourceFiles, recallSourceConversations, recallSourceThreadsAlias, recallSourceSummaries},
			"default":     recallSourceAll,
			"description": "Filter by source store for search/investigate. Default 'all'. 'threads' is accepted as a legacy alias for 'conversations'. 'summaries' searches past conversations' checkpoint summaries — a cheap first hop before fetching a full conversation.",
		},
		"current_conversation": map[string]interface{}{
			"type":        "boolean",
			"default":     false,
			"description": "search/investigate only: if true, scope retrieval (memories, files, and the conversation keyword scan) to the current conversation. Ignored by conversation mode, which targets a conversation ID or defaults to the current conversation.",
		},
		"next_page_token": map[string]interface{}{
			"type":        "string",
			"description": "Opaque cursor from a previous response. Pass it back to fetch the next page (fetch, conversation, origin, and lifecycle_events).",
		},
		"max_chunks": map[string]interface{}{
			"type":        "integer",
			"description": "Max chunks/messages/events to return. search/fetch default 5; conversation default 20; origin default 5 turns ending at the memory timestamp; lifecycle_events default 20 events per page.",
			"minimum":     1,
			"maximum":     findContextAbsoluteMaxChunks,
			"default":     recallDefaultMaxChunks,
		},
	},
	// No required fields: `mode` defaults to investigate, and each mode validates its own inputs
	// (query for search/investigate, target for fetch/related/origin) at call time.
	Required: []string{},
}

// recallStore is the narrow datastore surface recall needs. *datastore.Datastore satisfies it;
// tests inject a fake. Keeping the surface explicit documents recall's blast radius.
type recallStore interface {
	GetRelatedMemories(ctx context.Context, userID, chatID uuid.UUID, queryEmbedding []float32, activePersonalityID uuid.UUID) ([]*models.Memory, error)
	GetMemory(ctx context.Context, userID, id uuid.UUID) (*models.Memory, error)
	GetMemoryByIDPrefix(ctx context.Context, userID uuid.UUID, prefix string) (*models.Memory, error)
	GetRelatedFileChunks(ctx context.Context, userID uuid.UUID, personalityID *uuid.UUID, chatID *uuid.UUID, queryEmbedding []float32, limit int) ([]datastore.FileChunkResult, error)
	ListFileChunksForAttachment(ctx context.Context, fileAttachmentID uuid.UUID, limit int) ([]datastore.FileChunkResult, error)
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	GetChatMessage(ctx context.Context, userID, id uuid.UUID) (*models.ChatMessage, error)
	ListChatMessages(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error)
	ListChatMessageBookmarksPage(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int) (*models.PaginatedResponse, error)
	// ListChatMessagesAfter returns an ascending (sent_at, id) page strictly after the supplied
	// keyset position. Zero values select the conversation's first message.
	ListChatMessagesAfter(ctx context.Context, userID, chatID uuid.UUID, afterSentAt time.Time, afterID uuid.UUID, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error)
	// GetChatSummaryMemory and GetRelatedSummaryMemories back source_type=summaries search, fetch
	// by conversation ID, and the conversation-mode summary attach (see ADR 0x017).
	GetChatSummaryMemory(ctx context.Context, userID, chatID uuid.UUID) (*models.Memory, error)
	GetRelatedSummaryMemories(ctx context.Context, userID uuid.UUID, queryEmbedding []float32, limit int) ([]*models.Memory, error)
	// ListMemoryMergeEvents backs mode=lifecycle_events.
	ListMemoryMergeEvents(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MemoryMergeEventFilters) (*models.PaginatedResponse, error)
}

// RecallDistiller collapses retrieved material into a short answer for investigate mode.
// It is optional: when nil, investigate degrades gracefully to returning the raw material
// with a note. Implemented in the agent package (small archival model).
type RecallDistiller interface {
	Distill(ctx context.Context, question, material string) (string, error)
}

// RecallTool implements the unified `find_context` agent tool.
type RecallTool struct {
	store     recallStore
	embed     func(ctx context.Context, input string) ([]float32, error)
	distiller RecallDistiller
	// fileStore resolves image bytes for fetch-mode image attachments (see fetchImage).
	fileStore storage.FileStore
	logger    *zap.Logger
}

// NewRecallTool constructs a RecallTool. distiller may be nil (investigate falls back to raw
// material). fileStore may be nil (fetch-mode image loading then has no source bytes).
func NewRecallTool(ds *datastore.Datastore, oaiClient *openai.Client, distiller RecallDistiller, fileStore storage.FileStore, logger *zap.Logger) *RecallTool {
	return &RecallTool{
		store: ds,
		embed: func(ctx context.Context, input string) ([]float32, error) {
			return embedding.CreateEmbedding(ctx, oaiClient, input)
		},
		distiller: distiller,
		fileStore: fileStore,
		logger:    logger,
	}
}

// --- argument + result types ---------------------------------------------

type recallArgs struct {
	Mode                string `json:"mode"`
	Query               string `json:"query,omitempty"`
	Target              string `json:"target,omitempty"`
	TimeScope           string `json:"time_scope,omitempty"`
	SourceType          string `json:"source_type,omitempty"`
	CurrentConversation bool   `json:"current_conversation,omitempty"`
	NextPageToken       string `json:"next_page_token,omitempty"`
	MaxChunks           int    `json:"max_chunks,omitempty"`
}

func (a recallArgs) wantsCurrentConversation() bool {
	return a.CurrentConversation
}

type recallChunk struct {
	SourceType string `json:"source_type"` // "file" | "conversation" | "summary"
	Name       string `json:"name"`        // file name, conversation name, or chat name (summary)
	Index      int    `json:"index"`       // chunk sequence / message ordinal
	Text       string `json:"text"`
	// ConversationID is set on "summary" chunks: the conversation the summary was checkpointed
	// from, so the model can hop straight into conversation/fetch mode for the full source.
	ConversationID string `json:"conversation_id,omitempty"`
}

type recallMessage struct {
	Origin string `json:"origin"`
	SentAt string `json:"sent_at"`
	Text   string `json:"text"`

	// Cursor fields are intentionally not part of the tool payload. They retain the precise
	// database ordering values while SentAt remains a compact, human-readable RFC3339 string.
	id     uuid.UUID
	sentAt time.Time
}

// recallBookmark is a compact navigation row. Full content is returned only by its FetchTarget.
type recallBookmark struct {
	MessageID   string `json:"message_id"`
	Origin      string `json:"origin"`
	SentAt      string `json:"sent_at"`
	Snippet     string `json:"snippet"`
	FetchTarget string `json:"fetch_target"`
}

type recallConversation struct {
	ConversationID   string          `json:"conversation_id"`
	ConversationName string          `json:"conversation_name,omitempty"`
	Messages         []recallMessage `json:"messages"`
	// Summary is this conversation's checkpoint Summary memory content, when one exists.
	Summary string `json:"summary,omitempty"`
}

// recallLifecycleEvent is the agent-facing projection of a models.MemoryMergeEvent for
// mode=lifecycle_events. Snapshot is deliberately omitted — undo-machinery detail, not useful to
// the agent.
type recallLifecycleEvent struct {
	ID               string   `json:"id"`
	SurvivorMemoryID string   `json:"survivor_memory_id"`
	MergeType        string   `json:"merge_type"`
	Content          string   `json:"content"`
	DuplicatesFolded int      `json:"duplicates_folded,omitempty"`
	LinkGroupID      string   `json:"link_group_id,omitempty"`
	SourceMembers    []string `json:"source_members,omitempty"` // short content previews
	CreatedAt        string   `json:"created_at"`
	RevertedAt       string   `json:"reverted_at,omitempty"`
}

type recallResult struct {
	Mode            string                 `json:"mode"`
	SearchType      string                 `json:"search_type,omitempty"` // "semantic" for search (and investigate gather)
	Answer          string                 `json:"answer,omitempty"`
	Memories        []string               `json:"memories,omitempty"`
	Chunks          []recallChunk          `json:"chunks,omitempty"`
	Conversations   []recallConversation   `json:"conversations,omitempty"`
	Bookmarks       []recallBookmark       `json:"bookmarks,omitempty"`
	LifecycleEvents []recallLifecycleEvent `json:"lifecycle_events,omitempty"`
	Sources         []string               `json:"sources,omitempty"`
	TimeScope       *recallTimeScope       `json:"time_scope,omitempty"`
	Page            int                    `json:"page,omitempty"`
	TotalCount      int                    `json:"total_count,omitempty"`
	HasMore         bool                   `json:"has_more,omitempty"`
	NextPageToken   string                 `json:"next_page_token,omitempty"`
	Note            string                 `json:"note,omitempty"`
	Error           string                 `json:"error,omitempty"`
}

// setPageCursor fills the pagination envelope for 1-based page stores. next_page_token is set only
// when has_more is true — it always encodes the *next* page, never the current one, so a follow-up
// response never re-emits the request cursor.
func (r *recallResult) setPageCursor(page, pageSize, totalCount int) {
	if page < 1 {
		page = 1
	}
	r.Page = page
	if totalCount > 0 {
		r.TotalCount = totalCount
	}
	hasMore := pageSize > 0 && page*pageSize < totalCount
	r.HasMore = hasMore
	if !hasMore {
		return
	}
	tok := encodeCursor(recallCursor{Page: page + 1})
	r.NextPageToken = tok
}

// setConversationCursor advances an oldest-to-newest thread walk using the final database key,
// rather than an offset. TotalCount is the exact filtered count returned by the datastore; with
// the cursor's frozen page size it determines whether another page exists. Its frozen window and
// conversation ID prevent a follow-up tool call from accidentally applying a cursor to a different
// result set.
func (r *recallResult) setConversationCursor(page, pageSize, totalCount int, conversationID uuid.UUID, window timeWindow, messages []recallMessage) {
	if page < 1 {
		page = 1
	}
	r.Page = page
	if totalCount > 0 {
		r.TotalCount = totalCount
	}
	if pageSize <= 0 || len(messages) == 0 || page*pageSize >= totalCount {
		return
	}
	last := messages[len(messages)-1]
	if last.id == uuid.Nil || last.sentAt.IsZero() {
		return
	}
	cursor := recallCursor{
		Page:           page + 1,
		PageSize:       pageSize,
		AfterSentAt:    last.sentAt.UTC().Format(time.RFC3339Nano),
		AfterMessageID: last.id.String(),
		ConversationID: conversationID.String(),
	}
	if window.Min != nil {
		cursor.MinSentAt = window.Min.UTC().Format(time.RFC3339Nano)
	}
	if window.Max != nil {
		cursor.MaxSentAt = window.Max.UTC().Format(time.RFC3339Nano)
	}
	r.HasMore = true
	r.NextPageToken = encodeCursor(cursor)
}

// setOffsetCursor fills the pagination envelope for offset-based file chunk fetch. knownTotal may
// be 0 when the store only supports limit/over-fetch (has_more is then inferred from a full page).
func (r *recallResult) setOffsetCursor(offset, pageSize, fetchedThrough, knownTotal int) {
	if knownTotal > 0 {
		r.TotalCount = knownTotal
	}
	hasMore := pageSize > 0 && fetchedThrough >= offset+pageSize
	if knownTotal > 0 {
		hasMore = offset+pageSize < knownTotal
	}
	r.HasMore = hasMore
	if !hasMore {
		return
	}
	tok := encodeCursor(recallCursor{Offset: offset + pageSize})
	r.NextPageToken = tok
}

// recallTimeScope is the inspectable receipt for a natural-language time_scope input.
type recallTimeScope struct {
	Input           string `json:"input"`
	NormalizedStart string `json:"normalized_start,omitempty"`
	NormalizedEnd   string `json:"normalized_end,omitempty"`
	Timezone        string `json:"timezone"`
}

// recallCursor is the opaque, base64-encoded pagination state passed back via next_page_token.
// o is a file-fetch offset; p is a displayed page number. Conversation cursors additionally carry
// n (fixed page size), s/i (the final sent_at/message_id key), c (conversation ID), and min/max
// (the frozen SentAt filter bounds).
type recallCursor struct {
	Offset         int    `json:"o,omitempty"`
	Page           int    `json:"p,omitempty"`
	PageSize       int    `json:"n,omitempty"`
	AfterSentAt    string `json:"s,omitempty"`
	AfterMessageID string `json:"i,omitempty"`
	ConversationID string `json:"c,omitempty"`
	MinSentAt      string `json:"min,omitempty"`
	MaxSentAt      string `json:"max,omitempty"`
}

func encodeCursor(c recallCursor) string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(token string) (recallCursor, error) {
	if strings.TrimSpace(token) == "" {
		return recallCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return recallCursor{}, fmt.Errorf("invalid next_page_token")
	}
	var c recallCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return recallCursor{}, fmt.Errorf("invalid next_page_token")
	}
	// Reject negative bounds rather than silently clamping — a negative cursor is a malformed
	// token, not a valid position.
	if c.Offset < 0 || c.Page < 0 {
		return recallCursor{}, fmt.Errorf("invalid next_page_token")
	}
	return c, nil
}

// --- dispatch -------------------------------------------------------------

// Recall executes a find_context tool call. It returns the JSON tool result, any live memory rows
// surfaced (so the caller can persist them into turn context), and any
// file attachments to attach to the model's context (fetch mode, images only — see fetchImage).
func (t *RecallTool) Recall(ctx context.Context, chat *models.Chat, args []byte) (string, []*models.Memory, []*models.FileAttachment, error) {
	var a recallArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return t.fail("", fmt.Sprintf("invalid arguments: %v", err))
	}

	mode := strings.ToLower(strings.TrimSpace(a.Mode))
	if mode == "" {
		mode = recallModeInvestigate
	}
	if mode == recallModeThreadAlias {
		mode = recallModeConversation
	}
	if mode == recallModeMergeHistoryAlias {
		mode = recallModeLifecycleEvents
	}

	switch mode {
	case recallModeInvestigate:
		return t.investigate(ctx, chat, a)
	case recallModeSearch:
		return t.search(ctx, chat, a)
	case recallModeFetch:
		return t.fetch(ctx, chat, a)
	case recallModeRelated:
		return t.related(ctx, chat, a)
	case recallModeOrigin:
		return t.origin(ctx, chat, a)
	case recallModeConversation:
		return t.conversation(ctx, chat, a)
	case recallModeBookmarks:
		return t.bookmarks(ctx, chat, a)
	case recallModeLifecycleEvents:
		return t.lifecycleEvents(ctx, chat, a)
	default:
		return t.fail(mode, fmt.Sprintf("unknown mode %q; expected one of investigate, search, fetch, related, origin, conversation, bookmarks, lifecycle_events", a.Mode))
	}
}

// fail returns a logical failure (bad args, unknown mode, unparseable time_scope, not-found) as a
// structured Error payload with a NIL error. This is deliberate: the agent loop replaces a tool's
// output with err.Error() and flags IsErr whenever a handler returns a non-nil error, which would
// hide this Error field (and the mode) from the model and turn a recoverable "try again" into a hard
// tool failure. marshalToolResult only fails on non-encodable types, and recallResult is all strings
// — so the error is unreachable; if it somehow fired, fall back to a constant literal rather than
// return an empty output or a hard error.
func (t *RecallTool) fail(mode, msg string) (string, []*models.Memory, []*models.FileAttachment, error) {
	out, err := marshalToolResult(recallResult{Mode: mode, Error: msg}, recallToolName)
	if err != nil || out == "" {
		out = `{"mode":"` + recallToolName + `","error":"internal error encoding recall failure"}`
	}
	return out, nil, nil, nil
}

func (t *RecallTool) ok(res recallResult, memories []*models.Memory, attachments []*models.FileAttachment) (string, []*models.Memory, []*models.FileAttachment, error) {
	out, err := marshalToolResult(res, recallToolName)
	return out, memories, attachments, err
}

// --- shared helpers -------------------------------------------------------

func (t *RecallTool) maxChunks(requested, def int) int {
	n := requested
	if n <= 0 {
		n = def
	}
	if n > findContextAbsoluteMaxChunks {
		n = findContextAbsoluteMaxChunks
	}
	return n
}

// sourceType normalizes a raw source_type into a canonical value, mapping the legacy "threads"
// alias onto "conversations" so downstream code only ever compares against canonical constants.
func (t *RecallTool) sourceType(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case recallSourceMemories, recallSourceFiles, recallSourceSummaries, recallSourceAll:
		return s
	case recallSourceConversations, recallSourceThreadsAlias:
		return recallSourceConversations
	default:
		return recallSourceAll
	}
}

func personalityPtr(chat *models.Chat) *uuid.UUID {
	if chat.PersonalityID == uuid.Nil {
		return nil
	}
	pid := chat.PersonalityID
	return &pid
}

// wantsMemories / wantsFiles / wantsConversations decode a canonical source_type (see sourceType)
// into per-store booleans.
func wantsMemories(src string) bool { return src == recallSourceAll || src == recallSourceMemories }
func wantsFiles(src string) bool    { return src == recallSourceAll || src == recallSourceFiles }
func wantsConversations(src string) bool {
	return src == recallSourceAll || src == recallSourceConversations
}
func wantsSummaries(src string) bool { return src == recallSourceAll || src == recallSourceSummaries }

// formatMemories renders live memory rows for recall JSON: each line is prefixed with a hop-ready
// memory:<uuid> so related/origin can consume IDs from search/investigate without a second lookup.
func formatMemories(mems []*models.Memory) []string {
	out := make([]string, 0, len(mems))
	for _, m := range mems {
		if m == nil {
			continue
		}
		out = append(out, memorySourceLabel(m.ID)+" "+memoryutil.FormatMemoryForContext(m))
	}
	return out
}

// messagesFromPage converts a ListChatMessages page into chronological recallMessage values.
func messagesFromPage(page *models.PaginatedResponse) []recallMessage {
	if page == nil {
		return nil
	}
	msgs := make([]recallMessage, 0, len(page.Results))
	for _, item := range page.Results {
		m, ok := item.(*models.ChatMessage)
		if !ok || m == nil {
			continue
		}
		msgs = append(msgs, recallMessage{
			Origin: string(m.Origin),
			SentAt: m.SentAt.UTC().Format(time.RFC3339),
			Text:   m.Message,
			id:     m.ID,
			sentAt: m.SentAt,
		})
	}
	// ListChatMessages returns newest-first; present chronologically.
	sort.SliceStable(msgs, func(i, j int) bool {
		if msgs[i].sentAt.Equal(msgs[j].sentAt) {
			return msgs[i].id.String() < msgs[j].id.String()
		}
		return msgs[i].sentAt.Before(msgs[j].sentAt)
	})
	return msgs
}
