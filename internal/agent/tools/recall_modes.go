package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// retrieval bundles what a search pass gathered, so investigate and search can share one path.
type retrieval struct {
	memories []*models.Memory
	chunks   []recallChunk
	note     string
}

// withinWindow reports whether ts falls inside w. An open window (both bounds nil) matches
// everything. A set window rejects zero timestamps — "unknown time" is never claimed as in-range.
func withinWindow(ts time.Time, w timeWindow) bool {
	if w.Min == nil && w.Max == nil {
		return true
	}
	if ts.IsZero() {
		return false
	}
	if w.Min != nil && ts.Before(*w.Min) {
		return false
	}
	if w.Max != nil && ts.After(*w.Max) {
		return false
	}
	return true
}

// gather runs semantic retrieval over the requested stores for a query, honoring time_scope and
// current_conversation across every store (not just the conversation scan):
//   - memories: filtered by CreatedAt (window) and, when current_conversation, to this chat's memories;
//   - files: scoped to chat-only files when current_conversation, filtered by the attachment's upload time;
//   - conversation: a keyword+time scan of the current conversation (only when current_conversation —
//     there is no cross-conversation message vector index yet; conversation mode handles by-ID/time recall).
func (t *RecallTool) gather(ctx context.Context, chat *models.Chat, query, src string, maxChunks int, currentConversation bool, window timeWindow) (retrieval, error) {
	var r retrieval

	emb, err := t.embed(ctx, query)
	if err != nil {
		return r, fmt.Errorf("failed to embed query: %w", err)
	}

	if wantsMemories(src) {
		mems, err := t.store.GetRelatedMemories(ctx, chat.UserID, chat.ID, emb, chat.PersonalityID)
		if err != nil {
			t.logger.Error("recall: memory search failed", zap.Error(err))
			return r, fmt.Errorf("failed to search memories: %w", err)
		}
		for _, m := range mems {
			if m == nil {
				continue
			}
			if currentConversation && m.ChatID != chat.ID {
				continue // "this conversation only" excludes user-scoped / other-chat memories
			}
			if !withinWindow(m.CreatedAt, window) {
				continue
			}
			r.memories = append(r.memories, m)
		}
	}

	if wantsFiles(src) {
		// current_conversation narrows to chat-attached files (drop personality-scoped docs).
		pid := personalityPtr(chat)
		if currentConversation {
			pid = nil
		}
		// When time-scoping, over-fetch so the post-filter still has candidates to cap from.
		limit := maxChunks
		if window.Min != nil || window.Max != nil {
			limit = maxChunks * 4
			if limit > findContextAbsoluteMaxChunks {
				limit = findContextAbsoluteMaxChunks
			}
		}
		chunks, err := t.store.GetRelatedFileChunks(ctx, chat.UserID, pid, &chat.ID, emb, limit)
		if err != nil {
			if errors.Is(err, datastore.ErrNoFileSearchContext) {
				chunks = nil // no file context is not an error — just no file results
			} else {
				t.logger.Error("recall: file search failed", zap.Error(err))
				return r, fmt.Errorf("failed to search files: %w", err)
			}
		}
		added := 0
		for _, c := range chunks {
			if !withinWindow(c.CreatedAt, window) {
				continue
			}
			r.chunks = append(r.chunks, recallChunk{SourceType: "file", Name: c.FileName, Index: c.Sequence, Text: c.Content})
			if added++; added >= maxChunks {
				break
			}
		}
	}

	if wantsSummaries(src) {
		sums, err := t.store.GetRelatedSummaryMemories(ctx, chat.UserID, emb, maxChunks)
		if err != nil {
			t.logger.Error("recall: summary search failed", zap.Error(err))
			return r, fmt.Errorf("failed to search summaries: %w", err)
		}
		for _, m := range sums {
			if m == nil {
				continue
			}
			if !withinWindow(m.CreatedAt, window) {
				continue
			}
			r.chunks = append(r.chunks, recallChunk{SourceType: "summary", Name: m.ChatName, ConversationID: m.ChatID.String(), Text: m.Content})
		}
	}

	if wantsConversations(src) {
		if currentConversation {
			// Keyword + time scan of the current conversation (no per-message embeddings yet).
			filters := models.ChatMessageFilters{Query: strPtr(query), MinDate: window.Min, MaxDate: window.Max}
			page, err := t.store.ListChatMessages(ctx, chat.UserID, chat.ID, 1, maxChunks, filters)
			if err != nil {
				t.logger.Warn("recall: current-conversation scan failed", zap.Error(err))
			} else if page != nil {
				for i, m := range messagesFromPage(page) {
					r.chunks = append(r.chunks, recallChunk{SourceType: "conversation", Name: "current", Index: i, Text: m.Text})
				}
			}
		} else if src == recallSourceConversations {
			r.note = "Cross-conversation semantic search isn't available; use mode=\"conversation\" with a target conversation ID (or current_conversation) and a time_scope, or set current_conversation=true here."
		}
	}

	return r, nil
}

const recallSearchSemanticNote = "Results ranked by semantic similarity; this is not exact-match search."

// search returns raw retrieved material (chunks + memories) for a query.
func (t *RecallTool) search(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	query, ok := validateNonEmptyString(a.Query)
	if !ok {
		return t.fail(recallModeSearch, "query cannot be empty for search mode")
	}
	now := time.Now().UTC()
	window, err := parseTimeScope(a.TimeScope, now)
	if err != nil {
		return t.fail(recallModeSearch, err.Error())
	}
	src := t.sourceType(a.SourceType)
	maxChunks := t.maxChunks(a.MaxChunks, recallDefaultMaxChunks)

	r, err := t.gather(ctx, chat, query, src, maxChunks, a.wantsCurrentConversation(), window)
	if err != nil {
		return t.fail(recallModeSearch, err.Error())
	}

	res := recallResult{
		Mode:       recallModeSearch,
		SearchType: "semantic",
		Memories:   formatMemories(r.memories),
		Chunks:     r.chunks,
		TimeScope:  timeScopeReceipt(a.TimeScope, window, now),
		Note:       joinRecallNotes(recallSearchSemanticNote, r.note),
	}
	return t.ok(res, r.memories, nil)
}

// investigate retrieves material, then distills it into a short sourced answer. When no distiller
// is configured it degrades to returning the raw material with a note.
func (t *RecallTool) investigate(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	question, ok := validateNonEmptyString(a.Query)
	if !ok {
		return t.fail(recallModeInvestigate, "query (a question) cannot be empty for investigate mode")
	}
	now := time.Now().UTC()
	window, err := parseTimeScope(a.TimeScope, now)
	if err != nil {
		return t.fail(recallModeInvestigate, err.Error())
	}
	src := t.sourceType(a.SourceType)
	maxChunks := t.maxChunks(a.MaxChunks, recallDefaultMaxChunks)
	scopeReceipt := timeScopeReceipt(a.TimeScope, window, now)

	r, err := t.gather(ctx, chat, question, src, maxChunks, a.wantsCurrentConversation(), window)
	if err != nil {
		return t.fail(recallModeInvestigate, err.Error())
	}

	material, sources := buildDistillMaterial(r)
	if material == "" {
		note := "No relevant memories, files, or conversation material found for that question."
		if r.note != "" {
			note = r.note
		}
		return t.ok(recallResult{Mode: recallModeInvestigate, Note: note, TimeScope: scopeReceipt}, r.memories, nil)
	}

	// No distiller wired: return the raw material so the caller still gets the context.
	if t.distiller == nil {
		return t.ok(recallResult{
			Mode:      recallModeInvestigate,
			Memories:  formatMemories(r.memories),
			Chunks:    r.chunks,
			Sources:   sources,
			TimeScope: scopeReceipt,
			Note:      "Distillation unavailable; returning raw retrieved material.",
		}, r.memories, nil)
	}

	answer, err := t.distiller.Distill(ctx, question, material)
	if err != nil {
		t.logger.Warn("recall: distillation failed; returning raw material", zap.Error(err))
		return t.ok(recallResult{
			Mode:      recallModeInvestigate,
			Memories:  formatMemories(r.memories),
			Chunks:    r.chunks,
			Sources:   sources,
			TimeScope: scopeReceipt,
			Note:      "Distillation failed; returning raw retrieved material.",
		}, r.memories, nil)
	}

	return t.ok(recallResult{
		Mode:      recallModeInvestigate,
		Answer:    strings.TrimSpace(answer),
		Sources:   sources,
		TimeScope: scopeReceipt,
	}, r.memories, nil)
}

// fetch retrieves a specific item by memory/file ID or by filename. UUID targets are tried as a
// memory first, then as a file attachment; "memory:…" targets resolve only as memories; other
// non-UUID targets are matched by exact filename.
func (t *RecallTool) fetch(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	target, ok := validateNonEmptyString(a.Target)
	if !ok {
		return t.fail(recallModeFetch, "target (filename or ID) cannot be empty for fetch mode")
	}
	maxChunks := t.maxChunks(a.MaxChunks, recallDefaultMaxChunks)

	// Explicit memory:… tokens (full UUID or short prefix) always resolve as memories.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(target)), "memory:") {
		mem, err := t.resolveMemory(ctx, chat.UserID, target)
		if err != nil {
			return t.fail(recallModeFetch, err.Error())
		}
		return t.ok(recallResult{
			Mode:     recallModeFetch,
			Memories: formatMemories([]*models.Memory{mem}),
		}, []*models.Memory{mem}, nil)
	}

	// Explicit summary:<conversation-id> tokens resolve only as a conversation's checkpoint summary.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(target)), recallSummaryTargetPrefix) {
		idStr := strings.TrimSpace(target[len(recallSummaryTargetPrefix):])
		id, err := uuid.Parse(idStr)
		if err != nil {
			return t.fail(recallModeFetch, fmt.Sprintf("target %q must be summary:<conversation-uuid>", target))
		}
		return t.fetchSummary(ctx, chat.UserID, id)
	}

	if id, err := uuid.Parse(target); err == nil {
		// Ownership is enforced at the query: GetMemory, GetFileAttachment, and
		// GetChatSummaryMemory all filter by HasOwnerWith(userID), so an ID belonging to another
		// user resolves to not-found here — there is no UserID on the returned model to re-check,
		// and none is needed.
		// Try memory first.
		if mem, err := t.store.GetMemory(ctx, chat.UserID, id); err == nil && mem != nil {
			return t.ok(recallResult{
				Mode:     recallModeFetch,
				Memories: formatMemories([]*models.Memory{mem}),
			}, []*models.Memory{mem}, nil)
		}
		// Then a file attachment by ID.
		if fa, err := t.store.GetFileAttachment(ctx, chat.UserID, id); err == nil && fa != nil {
			return t.fetchFile(ctx, chat.UserID, fa, a.NextPageToken, maxChunks)
		}
		// Then a conversation's checkpoint summary, treating the ID as a chat ID.
		if sum, err := t.store.GetChatSummaryMemory(ctx, chat.UserID, id); err == nil && sum != nil {
			return t.ok(summaryChunkResult(sum, id), []*models.Memory{sum}, nil)
		}
		return t.fail(recallModeFetch, fmt.Sprintf("no memory, file, or conversation summary found for id %q", target))
	}

	// Filename match — exact only, to avoid the fuzzy filter resolving to the wrong file.
	fa, suggestions, err := t.resolveFileByName(ctx, chat.UserID, target)
	if err != nil {
		return t.fail(recallModeFetch, err.Error())
	}
	if fa == nil {
		note := fmt.Sprintf("No file exactly named %q.", target)
		if len(suggestions) > 0 {
			note += " Nearby files: " + strings.Join(suggestions, ", ") + ". Retry fetch with an exact name or the file's ID."
		}
		return t.ok(recallResult{Mode: recallModeFetch, Note: note}, nil, nil)
	}
	return t.fetchFile(ctx, chat.UserID, fa, a.NextPageToken, maxChunks)
}

// summaryChunkResult wraps a conversation's checkpoint Summary memory as a fetch-mode result
// chunk, carrying the conversation_id so the model can hop into conversation/fetch mode for the
// full source.
func summaryChunkResult(sum *models.Memory, chatID uuid.UUID) recallResult {
	return recallResult{
		Mode:   recallModeFetch,
		Chunks: []recallChunk{{SourceType: "summary", Name: sum.ChatName, ConversationID: chatID.String(), Text: sum.Content}},
	}
}

// fetchSummary resolves fetch mode for an explicit summary:<conversation-id> target.
func (t *RecallTool) fetchSummary(ctx context.Context, userID, chatID uuid.UUID) (string, []*models.Memory, []*models.FileAttachment, error) {
	sum, err := t.store.GetChatSummaryMemory(ctx, userID, chatID)
	if err != nil {
		return t.fail(recallModeFetch, err.Error())
	}
	if sum == nil {
		return t.ok(recallResult{Mode: recallModeFetch, Note: fmt.Sprintf("No checkpoint summary found for conversation %s.", chatID)}, nil, nil)
	}
	return t.ok(summaryChunkResult(sum, chatID), []*models.Memory{sum}, nil)
}

// fetchFile resolves fetch mode for a resolved file attachment. Images are attached directly to
// the model's context (see fetchImage) instead of being chunked; every other file type returns
// paginated text chunks as before.
func (t *RecallTool) fetchFile(ctx context.Context, userID uuid.UUID, fa *models.FileAttachment, token string, maxChunks int) (string, []*models.Memory, []*models.FileAttachment, error) {
	if strings.HasPrefix(fa.FileType, models.ImageMIMEPrefix) {
		return t.fetchImage(ctx, userID, fa)
	}
	return t.fetchFileChunks(ctx, fa.ID, fa.Name, token, maxChunks)
}

// fetchImage loads an image attachment's bytes and returns it as a tool-result attachment,
// routing it through the same context-attachment path as generate_image: the agent loop turns any
// FileAttachment with an image FileType + base64 FileContent into a vision payload
// (toolResultImagesFromAttachments), and the Claude adapter places tool-result images on a
// following *user* turn via AppendToolResults — Anthropic rejects image blocks on assistant/tool
// turns, so no adapter changes are needed here as long as FileContent is set.
func (t *RecallTool) fetchImage(ctx context.Context, userID uuid.UUID, fa *models.FileAttachment) (string, []*models.Memory, []*models.FileAttachment, error) {
	data, contentType := storage.ResolveAttachmentImageBytes(ctx, t.logger, t.fileStore, userID, fa, false)
	if len(data) == 0 {
		return t.ok(recallResult{
			Mode: recallModeFetch,
			Note: fmt.Sprintf("Found image %q but couldn't load its bytes.", fa.Name),
		}, nil, nil)
	}
	if contentType == "" {
		contentType = fa.FileType
	}

	// Copy so the caller's FileContent (often empty on DB-loaded attachments) is never mutated.
	att := *fa
	att.FileType = contentType
	att.FileContent = base64.StdEncoding.EncodeToString(data)

	return t.ok(recallResult{
		Mode: recallModeFetch,
		Note: "Image attached to your context for viewing.",
	}, nil, []*models.FileAttachment{&att})
}

// fetchFileChunks pages chunks for a file attachment. ListFileChunksForAttachment only supports a
// limit, so we over-fetch (offset+limit) and slice — fine at v1 file sizes; see ADR follow-ups.
func (t *RecallTool) fetchFileChunks(ctx context.Context, faID uuid.UUID, name, token string, maxChunks int) (string, []*models.Memory, []*models.FileAttachment, error) {
	cur, err := decodeCursor(token)
	if err != nil {
		return t.fail(recallModeFetch, err.Error())
	}
	offset := cur.Offset // decodeCursor guarantees offset >= 0

	rows, err := t.store.ListFileChunksForAttachment(ctx, faID, offset+maxChunks)
	if err != nil {
		return t.fail(recallModeFetch, fmt.Sprintf("failed to load file chunks: %v", err))
	}

	start := min(offset, len(rows))
	end := min(offset+maxChunks, len(rows))
	chunks := make([]recallChunk, 0, end-start)
	for i := start; i < end; i++ {
		c := rows[i]
		chunks = append(chunks, recallChunk{SourceType: "file", Name: c.FileName, Index: c.Sequence, Text: c.Content})
	}

	res := recallResult{Mode: recallModeFetch, Chunks: chunks}
	res.setOffsetCursor(offset, maxChunks, len(rows), 0)
	if len(chunks) == 0 {
		res.Note = fmt.Sprintf("No further chunks for %q.", name)
	}
	return t.ok(res, nil, nil)
}

// resolveFileByName returns the exact (case-insensitive) filename match. ListFileAttachments' Name
// filter is fuzzy (also matches description / chat / personality names), so we never fall back to a
// non-exact hit; instead we hand back the candidate names as suggestions for the model to retry.
func (t *RecallTool) resolveFileByName(ctx context.Context, userID uuid.UUID, name string) (match *models.FileAttachment, suggestions []string, err error) {
	page, err := t.store.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{Name: strPtr(name)})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to look up file %q: %v", name, err)
	}
	if page == nil || len(page.Results) == 0 {
		return nil, nil, nil
	}
	seen := make(map[string]struct{})
	for _, item := range page.Results {
		fa, ok := item.(*models.FileAttachment)
		if !ok || fa == nil {
			continue
		}
		if strings.EqualFold(fa.Name, name) {
			return fa, nil, nil // exact match wins immediately
		}
		if _, dup := seen[fa.Name]; !dup && fa.Name != "" {
			seen[fa.Name] = struct{}{}
			suggestions = append(suggestions, fa.Name)
		}
	}
	return nil, suggestions, nil
}

// related turns one memory into its cluster: use the memory's content as a semantic query.
func (t *RecallTool) related(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	mem, err := t.resolveMemory(ctx, chat.UserID, a.Target)
	if err != nil {
		return t.fail(recallModeRelated, err.Error())
	}

	emb, err := t.embed(ctx, mem.Content)
	if err != nil {
		return t.fail(recallModeRelated, fmt.Sprintf("failed to embed memory content: %v", err))
	}
	related, err := t.store.GetRelatedMemories(ctx, chat.UserID, chat.ID, emb, chat.PersonalityID)
	if err != nil {
		return t.fail(recallModeRelated, fmt.Sprintf("failed to find related memories: %v", err))
	}

	// Exclude the seed memory itself.
	out := make([]*models.Memory, 0, len(related))
	for _, m := range related {
		if m != nil && m.ID != mem.ID {
			out = append(out, m)
		}
	}

	res := recallResult{
		Mode:       recallModeRelated,
		SearchType: "semantic",
		Memories:   formatMemories(out),
		Note:       recallSearchSemanticNote,
	}
	if len(out) == 0 {
		res.Note = joinRecallNotes(recallSearchSemanticNote, "No related memories found for that memory.")
	}
	return t.ok(res, out, nil)
}

// origin is the simple "I have a memory ID — show me where it came from" path: resolve the memory,
// then return the last N turns of its source conversation ending at memory.CreatedAt (default 5).
// No per-message FK exists on Memory; ChatID + CreatedAt is the provenance we have.
func (t *RecallTool) origin(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	mem, err := t.resolveMemory(ctx, chat.UserID, a.Target)
	if err != nil {
		return t.fail(recallModeOrigin, err.Error())
	}
	if mem.ChatID == uuid.Nil {
		return t.ok(recallResult{
			Mode:     recallModeOrigin,
			Memories: formatMemories([]*models.Memory{mem}),
			Note:     "This memory has no originating conversation (user-scoped with no source chat).",
		}, []*models.Memory{mem}, nil)
	}

	maxMsgs := t.maxChunks(a.MaxChunks, recallDefaultOriginMsgs)

	cur, err := decodeCursor(a.NextPageToken)
	if err != nil {
		return t.fail(recallModeOrigin, err.Error())
	}
	page := cur.Page
	if page < 1 {
		page = 1
	}

	// Messages at or before the memory's creation time are the source that produced it. Ordered
	// newest-first by the store then flipped chronological, this yields the conversation tail
	// leading up to the memory.
	createdAt := mem.CreatedAt
	filters := models.ChatMessageFilters{}
	if !createdAt.IsZero() {
		filters.MaxDate = &createdAt
	}
	conv, total, err := t.loadConversation(ctx, chat.UserID, mem.ChatID, page, maxMsgs, filters)
	if err != nil {
		return t.fail(recallModeOrigin, err.Error())
	}

	res := recallResult{
		Mode:          recallModeOrigin,
		Memories:      formatMemories([]*models.Memory{mem}),
		Conversations: []recallConversation{conv},
	}
	res.setPageCursor(page, maxMsgs, total)
	notes := []string{}
	if page == 1 && a.MaxChunks <= 0 {
		notes = append(notes, recallOriginWindowNote)
	}
	if len(conv.Messages) == 0 {
		notes = append(notes, "No messages in the source conversation at or before this memory's creation time.")
	} else if res.HasMore {
		notes = append(notes, "More earlier turns available — pass next_page_token for the next page.")
	}
	res.Note = joinRecallNotes(notes...)
	return t.ok(res, []*models.Memory{mem}, nil)
}

// conversation recovers conversation history for ONE conversation: the target conversation ID, or
// the current conversation by default (including the "current_conversation" sentinel). time_scope
// filters messages within that conversation by SentAt.
// (Cross-conversation time sweeps wait on a future list_conversations tool.)
func (t *RecallTool) conversation(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	cur, err := decodeCursor(a.NextPageToken)
	if err != nil {
		return t.fail(recallModeConversation, err.Error())
	}
	maxMsgs := t.maxChunks(a.MaxChunks, recallDefaultConversationMsgs)
	if cur.PageSize != 0 {
		if cur.PageSize < 1 || cur.PageSize > findContextAbsoluteMaxChunks {
			return t.fail(recallModeConversation, "invalid next_page_token")
		}
		// Retain a stable page size across a keyset walk so page metadata and has_more remain
		// meaningful even if a caller changes max_chunks while following the cursor.
		maxMsgs = cur.PageSize
	}
	page := cur.Page
	if page < 1 {
		page = 1
	}

	// Resolve the target conversation: explicit conversation ID or the current_conversation
	// sentinel (case-insensitive), else default to the current conversation.
	chatID := chat.ID
	if tgt := strings.TrimSpace(a.Target); tgt != "" {
		switch strings.ToLower(tgt) {
		case recallTargetCurrentConversation:
			// Sentinel for "current conversation" — chatID already defaults to chat.ID.
		default:
			id, perr := uuid.Parse(tgt)
			if perr != nil {
				return t.fail(recallModeConversation, "target must be a valid conversation ID")
			}
			chatID = id
		}
	}

	if cur.ConversationID != "" && cur.ConversationID != chatID.String() {
		return t.fail(recallModeConversation, "next_page_token belongs to a different conversation")
	}

	now := time.Now().UTC()
	window, err := conversationWindow(a.TimeScope, cur, now)
	if err != nil {
		return t.fail(recallModeConversation, err.Error())
	}

	afterSentAt, afterMessageID, err := conversationCursorPosition(cur)
	if err != nil {
		return t.fail(recallModeConversation, err.Error())
	}
	filters := models.ChatMessageFilters{MinDate: window.Min, MaxDate: window.Max}
	conv, total, err := t.loadConversationChronological(ctx, chat.UserID, chatID, afterSentAt, afterMessageID, maxMsgs, filters)
	if err != nil {
		return t.fail(recallModeConversation, err.Error())
	}
	if sum, err := t.store.GetChatSummaryMemory(ctx, chat.UserID, chatID); err != nil {
		t.logger.Warn("recall: conversation summary lookup failed", zap.Error(err))
	} else if sum != nil {
		conv.Summary = sum.Content
	}

	res := recallResult{
		Mode:          recallModeConversation,
		Conversations: []recallConversation{conv},
		TimeScope:     timeScopeReceipt(a.TimeScope, window, now),
	}
	res.setConversationCursor(page, maxMsgs, total, chatID, window, conv.Messages)
	if len(conv.Messages) == 0 {
		if window.Min != nil || window.Max != nil {
			res.Note = "No messages in that conversation for the given time_scope."
		} else {
			res.Note = "No messages found for that conversation."
		}
	}
	return t.ok(res, nil, nil)
}

// conversationWindow freezes resolved time bounds into the cursor. A relative scope such as
// "last 7 days" therefore cannot slide between pages and silently change the result set.
func conversationWindow(input string, cur recallCursor, now time.Time) (timeWindow, error) {
	if cur.MinSentAt != "" || cur.MaxSentAt != "" {
		window := timeWindow{}
		if cur.MinSentAt != "" {
			min, err := time.Parse(time.RFC3339Nano, cur.MinSentAt)
			if err != nil {
				return timeWindow{}, fmt.Errorf("invalid next_page_token")
			}
			window.Min = &min
		}
		if cur.MaxSentAt != "" {
			max, err := time.Parse(time.RFC3339Nano, cur.MaxSentAt)
			if err != nil {
				return timeWindow{}, fmt.Errorf("invalid next_page_token")
			}
			window.Max = &max
		}
		return window, nil
	}
	return parseTimeScope(input, now)
}

func conversationCursorPosition(cur recallCursor) (time.Time, uuid.UUID, error) {
	if cur.AfterSentAt == "" && cur.AfterMessageID == "" {
		// A continuation must carry the final key from its preceding page. A bare page number is
		// the retired offset-pagination format and cannot safely resume a keyset walk.
		if cur.Page > 1 {
			return time.Time{}, uuid.Nil, fmt.Errorf("invalid next_page_token")
		}
		return time.Time{}, uuid.Nil, nil
	}
	if cur.AfterSentAt == "" || cur.AfterMessageID == "" {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid next_page_token")
	}
	sentAt, err := time.Parse(time.RFC3339Nano, cur.AfterSentAt)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid next_page_token")
	}
	id, err := uuid.Parse(cur.AfterMessageID)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid next_page_token")
	}
	return sentAt, id, nil
}

// lifecycleEvents lists memory lifecycle audit rows (fold/link and related merge-audit events) —
// a paginated list, not semantic search. Input alias mode=merge_history is normalized to
// lifecycle_events before dispatch.
func (t *RecallTool) lifecycleEvents(ctx context.Context, chat *models.Chat, a recallArgs) (string, []*models.Memory, []*models.FileAttachment, error) {
	pageSize := t.maxChunks(a.MaxChunks, recallDefaultLifecyclePageSize)

	cur, err := decodeCursor(a.NextPageToken)
	if err != nil {
		return t.fail(recallModeLifecycleEvents, err.Error())
	}
	page := cur.Page
	if page < 1 {
		page = 1
	}

	now := time.Now().UTC()
	window, err := parseTimeScope(a.TimeScope, now)
	if err != nil {
		return t.fail(recallModeLifecycleEvents, err.Error())
	}

	filters := models.MemoryMergeEventFilters{ExcludeReverted: true, MinDate: window.Min, MaxDate: window.Max}
	if q, ok := validateNonEmptyString(a.Query); ok {
		filters.Query = &q
	}
	if tgt := strings.TrimSpace(a.Target); tgt != "" {
		mem, err := t.resolveMemory(ctx, chat.UserID, tgt)
		if err != nil {
			return t.fail(recallModeLifecycleEvents, err.Error())
		}
		filters.SurvivorMemoryID = &mem.ID
	}

	events, err := t.store.ListMemoryMergeEvents(ctx, chat.UserID, page, pageSize, filters)
	if err != nil {
		return t.fail(recallModeLifecycleEvents, fmt.Sprintf("failed to list lifecycle events: %v", err))
	}

	out := make([]recallLifecycleEvent, 0, len(events.Results))
	for _, item := range events.Results {
		ev, ok := item.(*models.MemoryMergeEvent)
		if !ok || ev == nil {
			continue
		}
		out = append(out, toRecallLifecycleEvent(ev))
	}

	res := recallResult{
		Mode:            recallModeLifecycleEvents,
		LifecycleEvents: out,
		TimeScope:       timeScopeReceipt(a.TimeScope, window, now),
	}
	res.setPageCursor(page, pageSize, events.TotalCount)
	if len(out) == 0 {
		res.Note = "No lifecycle events matched."
	} else if res.HasMore {
		res.Note = "More lifecycle events available — pass next_page_token for the next page."
	}
	return t.ok(res, nil, nil)
}

// toRecallLifecycleEvent projects a models.MemoryMergeEvent into the agent-facing shape, dropping
// Snapshot (undo-machinery detail) and truncating source member content to short previews.
func toRecallLifecycleEvent(ev *models.MemoryMergeEvent) recallLifecycleEvent {
	out := recallLifecycleEvent{
		ID:               ev.ID.String(),
		SurvivorMemoryID: ev.SurvivorMemoryID.String(),
		MergeType:        string(ev.MergeType),
		Content:          ev.Content,
		DuplicatesFolded: ev.DuplicatesFolded,
		CreatedAt:        ev.CreatedAt.UTC().Format(time.RFC3339),
	}
	if ev.LinkGroupID != nil {
		out.LinkGroupID = ev.LinkGroupID.String()
	}
	if ev.RevertedAt != nil {
		out.RevertedAt = ev.RevertedAt.UTC().Format(time.RFC3339)
	}
	for _, m := range ev.SourceMembers {
		out.SourceMembers = append(out.SourceMembers, truncateRunes(m.Content, recallLifecyclePreviewLen))
	}
	return out
}

func (t *RecallTool) loadConversation(ctx context.Context, userID, chatID uuid.UUID, page, pageSize int, filters models.ChatMessageFilters) (recallConversation, int, error) {
	res, err := t.store.ListChatMessages(ctx, userID, chatID, page, pageSize, filters)
	if err != nil {
		return recallConversation{}, 0, fmt.Errorf("failed to load conversation %s: %v", chatID, err)
	}
	total := 0
	if res != nil {
		total = res.TotalCount
	}
	msgs := messagesFromPage(res)
	// Conversation name isn't carried on ChatMessage; single-conversation loads leave it empty
	// rather than issuing an extra chat lookup. (A future list_conversations tool will own naming.)
	return recallConversation{ConversationID: chatID.String(), Messages: msgs}, total, nil
}

func (t *RecallTool) loadConversationChronological(ctx context.Context, userID, chatID uuid.UUID, afterSentAt time.Time, afterID uuid.UUID, pageSize int, filters models.ChatMessageFilters) (recallConversation, int, error) {
	res, err := t.store.ListChatMessagesAfter(ctx, userID, chatID, afterSentAt, afterID, pageSize, filters)
	if err != nil {
		return recallConversation{}, 0, fmt.Errorf("failed to load conversation %s: %v", chatID, err)
	}
	total := 0
	if res != nil {
		total = res.TotalCount
	}
	return recallConversation{ConversationID: chatID.String(), Messages: messagesFromPage(res)}, total, nil
}

// buildDistillMaterial flattens retrieved memories + chunks into a numbered material block and a
// parallel list of short source labels the answer can cite.
func buildDistillMaterial(r retrieval) (material string, sources []string) {
	var b strings.Builder
	n := 0
	add := func(label, text string) {
		if n >= recallDistillMaxSources {
			return
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		text = truncateRunes(text, recallDistillMaxSourceLen)
		n++
		fmt.Fprintf(&b, "[%d] (%s) %s\n\n", n, label, text)
		sources = append(sources, label)
	}

	for _, m := range r.memories {
		if m == nil {
			continue
		}
		add(memorySourceLabel(m.ID), m.Content)
	}
	for _, c := range r.chunks {
		label := fmt.Sprintf("%s:%s#%d", c.SourceType, c.Name, c.Index)
		add(label, c.Text)
	}
	return strings.TrimSpace(b.String()), sources
}

// truncateRunes caps s to at most maxRunes runes, appending an ellipsis when it trims. It counts by
// rune (not byte) so a multibyte character is never split into invalid UTF-8 — which would otherwise
// be handed to the distillation model.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	// Fast path: len (bytes) >= rune count, so if byte length fits, the string fits.
	if len(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func joinRecallNotes(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func strPtr(s string) *string { return &s }
