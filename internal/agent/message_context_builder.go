package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

const (
	carryOverMaxTurns  = 3
	carryOverMaxTokens = 500
)

// messageContextBuilder is the single owner of request-context construction for chat turns.
// It builds provider-specific request payloads from shared chat state, history, and user input.
type messageContextBuilder struct {
	ds                   *datastore.Datastore
	fileStore            storage.FileStore
	telemetry            *telemetry.Telemetry
	tokenCounter         *provider.TokenCounter
	loadHistoryOverride  func(ctx context.Context, userID, chatID, excludeMessageID uuid.UUID, pageSize int, minDate *time.Time, logContext string) []*models.ChatMessage
	expressionThumbCache *expressionPortraitThumbCache
}

func newMessageContextBuilder(ds *datastore.Datastore, tel *telemetry.Telemetry, fileStore storage.FileStore, loadHistoryOverride func(ctx context.Context, userID, chatID, excludeMessageID uuid.UUID, pageSize int, minDate *time.Time, logContext string) []*models.ChatMessage, expressionThumbCache *expressionPortraitThumbCache) (*messageContextBuilder, error) {
	if tel == nil || tel.Logger == nil {
		return nil, fmt.Errorf("messageContextBuilder: telemetry and telemetry.Logger must be non-nil")
	}
	return &messageContextBuilder{
		ds:                   ds,
		fileStore:            fileStore,
		telemetry:            tel,
		tokenCounter:         provider.NewTokenCounter(),
		loadHistoryOverride:  loadHistoryOverride,
		expressionThumbCache: expressionThumbCache,
	}, nil
}

// messageContextBuildRequest holds all inputs for a single chat-turn context build.
type messageContextBuildRequest struct {
	UserID             uuid.UUID
	Chat               *models.Chat
	UserPrompt         string
	CurrentMessage     *models.ChatMessage
	Memories           []string
	LiveMemories       []*models.Memory
	ActiveMood         *models.Mood
	ActiveMoodRituals  []*models.Ritual
	IsAutoMood         bool
	MoodToolsAvailable bool
	Attachments        []*models.FileAttachment
	// ImageBytes maps attachment ID → raw image bytes for Claude vision. Nil on the
	// OpenAI path; the OpenAI renderer uses FileID and ignores RawBytes entirely.
	ImageBytes map[uuid.UUID][]byte

	ExpressionsEnabled         bool
	IncludeAttachmentContext   bool
	AdditionalDeveloperContext string
	// LoadHistoryImageBytes controls whether prior-turn image attachments are loaded
	// as raw bytes for Claude vision. OpenAI uses FileID from attachment records.
	LoadHistoryImageBytes bool
}

func (b *messageContextBuilder) build(ctx context.Context, req messageContextBuildRequest) (*provider.ModelContext, error) {
	var excludeID *uuid.UUID
	if req.CurrentMessage != nil {
		excludeID = &req.CurrentMessage.ID
	}
	modelCtx, carryOver, history, err := b.buildHistoryContext(ctx, req.UserID, req.Chat, excludeID, req.LoadHistoryImageBytes)
	if err != nil {
		return nil, err
	}
	appendMergedAdditionalContext(modelCtx, mergeAdditionalContextItems(carryOver, history, req.CurrentMessage, req.Memories, req.LiveMemories))
	if req.IncludeAttachmentContext && len(req.Attachments) > 0 {
		if hint := b.buildFullAttachmentContext(ctx, req.UserID, req.Chat.ID, req.Attachments); hint != "" {
			modelCtx.Append(provider.SegmentKindAttachmentContext, provider.RoleDeveloper, hint, false)
		}
	}
	appendMoodContextForChatTurn(modelCtx, req.ActiveMood, req.ActiveMoodRituals, moodContextOptions{
		IsAutoMood:         req.IsAutoMood,
		MoodToolsAvailable: req.MoodToolsAvailable,
	})
	if req.ExpressionsEnabled {
		b.appendPriorTurnExpressionContinuity(ctx, req.UserID, modelCtx, carryOver, history)
	}
	if extra := strings.TrimSpace(req.AdditionalDeveloperContext); extra != "" {
		modelCtx.Append(provider.SegmentKindDeveloperContext, provider.RoleDeveloper, extra, false)
	}
	modelCtx.AppendUserMessage(provider.RoleUser, req.UserPrompt, userMessageImagesFromAttachments(req.Attachments, req.ImageBytes), false)
	return modelCtx, nil
}

// userMessageImagesFromAttachments collects image metadata for vision-capable providers.
// For OpenAI the FileID is used; for Claude, RawBytes sourced from imageBytes (keyed by
// attachment ID) are injected. imageBytes may be nil (OpenAI path).
//
// An attachment is included when it has at least one of:
//   - a.FileID set   → OpenAI vision can use it
//   - RawBytes found → Claude vision can use it
//
// Attachments with neither (e.g. older generated images on the OpenAI path, or
// pre-migration uploads where imageBytes is nil) are silently skipped rather than
// producing an empty image block that could confuse the model.
func userMessageImagesFromAttachments(attachments []*models.FileAttachment, imageBytes map[uuid.UUID][]byte) []provider.UserMessageImage {
	var out []provider.UserMessageImage
	for _, a := range attachments {
		if a == nil {
			continue
		}
		if !strings.HasPrefix(a.FileType, models.ImageMIMEPrefix) {
			continue
		}
		rawBytes := imageBytes[a.ID]
		hasBytes := len(rawBytes) > 0
		if a.FileID == nil && !hasBytes {
			continue
		}
		img := provider.UserMessageImage{
			MediaType: a.FileType,
			RawBytes:  rawBytes,
		}
		if a.FileID != nil {
			img.FileID = *a.FileID
		}
		out = append(out, img)
	}
	return out
}

// buildAttachmentContextHint renders the developer-facing hint about files the user
// attached to the current message. It separates images (already rendered inline as
// vision blocks) from documents, and nudges the model toward find_context for document
// content — keyed on the exact file names shown — rather than guessing keywords.
// Returns "" when there is nothing worth announcing.
func buildAttachmentContextHint(attachments []*models.FileAttachment) string {
	var images, docs []string
	for _, a := range attachments {
		if a == nil {
			continue
		}
		label := strings.TrimSpace(a.Name)
		if label == "" {
			label = "(unnamed file)"
		}
		if ft := strings.TrimSpace(a.FileType); ft != "" {
			label += " (" + ft + ")"
		}
		if a.Description != nil {
			if d := strings.TrimSpace(*a.Description); d != "" {
				label += " — " + d
			}
		}
		if strings.HasPrefix(a.FileType, models.ImageMIMEPrefix) {
			images = append(images, label)
		} else {
			docs = append(docs, label)
		}
	}

	if len(images) == 0 && len(docs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("The user attached the following file(s) to this message:")
	for _, l := range images {
		fmt.Fprintf(&b, "\n- %s [image]", l)
	}
	for _, l := range docs {
		fmt.Fprintf(&b, "\n- %s [document]", l)
	}
	if len(images) > 0 {
		b.WriteString("\n\nImage attachments are already visible to you inline in this message.")
	}
	if len(docs) > 0 {
		b.WriteString("\n\nDocument attachments are NOT inlined — their text lives in the file store. " +
			"To read one, call find_context with mode=\"fetch\" and the exact file name shown above. " +
			"Prefer find_context over guessing when the user asks about an attached document.")
	}
	return b.String()
}

// loadHistoryMessages returns messages in chronological (oldest-first) order for use
// as inline conversation history. It scopes the fetch to messages on or after minDate
// (pass nil to load all messages). The current message (excludeMessageID) is excluded
// since it is appended separately as the final turn.
func (b *messageContextBuilder) loadHistoryMessages(ctx context.Context, userID, chatID uuid.UUID, excludeMessageID *uuid.UUID, pageSize int, minDate *time.Time, logContext string) []*models.ChatMessage {
	if b.loadHistoryOverride != nil {
		id := uuid.Nil
		if excludeMessageID != nil {
			id = *excludeMessageID
		}
		return b.loadHistoryOverride(ctx, userID, chatID, id, pageSize, minDate, logContext)
	}
	msgs := b.fetchRecentMessages(ctx, userID, chatID, excludeMessageID, pageSize, models.ChatMessageFilters{MinDate: minDate}, logContext)

	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs
}

// fetchRecentMessages fetches up to pageSize messages from the DB (newest-first),
// filtering out the excluded message ID and empty-body rows.
// filters is applied directly to the DB query (e.g. MinDate, MaxDate).
// logContext is included in the debug log on error.
func (b *messageContextBuilder) fetchRecentMessages(ctx context.Context, userID, chatID uuid.UUID, excludeMessageID *uuid.UUID, pageSize int, filters models.ChatMessageFilters, logContext string) []*models.ChatMessage {
	resp, err := b.ds.ListChatMessages(ctx, userID, chatID, 1, pageSize, filters)
	if err != nil {
		b.telemetry.Logger.Info(logContext+": failed to fetch messages", zap.Error(err),
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
		)
		// Best-effort semantics: surface an empty slice to callers so they can
		// continue composing context without hard-failing the turn.
		return []*models.ChatMessage{}
	}
	out := make([]*models.ChatMessage, 0, len(resp.Results))
	for _, item := range resp.Results {
		msg, ok := item.(*models.ChatMessage)
		if !ok || msg == nil || (excludeMessageID != nil && msg.ID == *excludeMessageID) || strings.TrimSpace(msg.Message) == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// selectCarryOverTurns is the canonical carry-over selector used across providers.
// It enforces consistent fetch and token-window behavior for checkpoint continuity.
func (b *messageContextBuilder) selectCarryOverTurns(
	ctx context.Context,
	userID, chatID uuid.UUID,
	excludeMessageID *uuid.UUID,
	maxTurns, maxTokens int,
	lastCheckpointAt *time.Time,
	logContext string,
) ([][2]*models.ChatMessage, error) {
	if maxTurns <= 0 || maxTokens <= 0 {
		return nil, fmt.Errorf("max turns or max tokens is less than 0")
	}
	recent := b.fetchRecentMessages(ctx, userID, chatID, excludeMessageID, 50, models.ChatMessageFilters{MaxDate: lastCheckpointAt}, logContext)

	return b.tokenCounter.SelectCarryOverTurns(recent, maxTurns, maxTokens), nil
}

func (b *messageContextBuilder) buildHistoryContext(ctx context.Context, userID uuid.UUID, chat *models.Chat, excludeMessageID *uuid.UUID, loadHistoryImageBytes bool) (
	modelCtx *provider.ModelContext,
	carryOver [][2]*models.ChatMessage,
	history []*models.ChatMessage,
	err error,
) {
	modelCtx = &provider.ModelContext{}
	sysText := mergePrompts(baseSystemPrompt, chat.SystemPrompt)
	modelCtx.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, sysText, true)

	if chat.CheckpointSummary != "" {
		modelCtx.Append(provider.SegmentKindCheckpointSummary, provider.RoleDeveloper, chat.CheckpointSummary, true)
	}
	if chat.Scratchpad != "" {
		modelCtx.Append(provider.SegmentKindScratchpad, provider.RoleDeveloper, chat.Scratchpad, true)
	}

	if chat.LastCheckpointAt != nil {
		carryOver, err = b.selectCarryOverTurns(ctx, userID, chat.ID, excludeMessageID, carryOverMaxTurns, carryOverMaxTokens, chat.LastCheckpointAt, "checkpoint carry-over")
		if err != nil {
			return nil, nil, nil, err
		}
	}
	history = b.loadHistoryMessages(ctx, userID, chat.ID, excludeMessageID, 50, chat.LastCheckpointAt, "history")

	tz, _ := middleware.GetClientTimezoneFromContext(ctx)
	normalizedTZ := normalizeTimezoneName(tz)

	if chat.LastCheckpointAt != nil {
		for _, t := range carryOver {
			b.appendHistoryMessage(ctx, modelCtx, userID, chat.ID, t[0], normalizedTZ, loadHistoryImageBytes)
			b.appendHistoryMessage(ctx, modelCtx, userID, chat.ID, t[1], normalizedTZ, loadHistoryImageBytes)
		}
	}
	for _, msg := range history {
		b.appendHistoryMessage(ctx, modelCtx, userID, chat.ID, msg, normalizedTZ, loadHistoryImageBytes)
	}

	// Carry prior-turn tool results back into context so they don't vanish the moment
	// they scroll off (redundant re-fetching, lost subagent output, images, etc.).
	// Scoped to the post-checkpoint history window: pre-checkpoint context is already
	// compressed into the checkpoint summary, so replaying old tool dumps there would
	// only add noise. Ages are measured in assistant turns within this window. Appended
	// after the (cacheable) history turns so the block never fragments the cache prefix.
	appendPersistedToolResults(modelCtx, selectPersistedToolResults(assistantTurnsFromHistory(history)))

	return modelCtx, carryOver, history, nil
}

func (b *messageContextBuilder) appendHistoryMessage(
	ctx context.Context,
	modelCtx *provider.ModelContext,
	userID, chatID uuid.UUID,
	msg *models.ChatMessage,
	normalizedTZ string,
	loadImageBytes bool,
) {
	if modelCtx == nil || msg == nil {
		return
	}
	role := provider.RoleAssistant
	body := msg.Message
	if msg.Origin == models.MessageOriginUser {
		role = provider.RoleUser
		body = formatUserMessageWithTime(msg.SentAt, normalizedTZ, msg.Message)
	}
	images := historyImagesForMessage(ctx, b.telemetry.Logger, b.fileStore, userID, chatID, msg, loadImageBytes)
	modelCtx.AppendHistoryTurn(role, body, images, true)
}

// assistantTurnsFromHistory returns the assistant messages from a chronological history
// slice, preserving order. Used to age persisted tool results in assistant-turn units.
func assistantTurnsFromHistory(history []*models.ChatMessage) []*models.ChatMessage {
	out := make([]*models.ChatMessage, 0, len(history))
	for _, msg := range history {
		if msg != nil && msg.Origin == models.MessageOriginAssistant {
			out = append(out, msg)
		}
	}
	return out
}

// appendPriorTurnExpressionContinuity injects developer context about the last classified expression,
// and attaches the expression portrait thumbnail when available so the model sees the same icon as the UI.
func (b *messageContextBuilder) appendPriorTurnExpressionContinuity(ctx context.Context, userID uuid.UUID, modelCtx *provider.ModelContext, carryOver [][2]*models.ChatMessage, history []*models.ChatMessage) {
	if modelCtx == nil {
		b.telemetry.Logger.Warn("appendPriorTurnExpressionContinuity: modelCtx is nil")
		return
	}
	src := priorAssistantMessageWithExpression(history, carryOver)
	if src == nil {
		return
	}
	key := strings.TrimSpace(*src.GenerationExpressionKey)
	labelText := derefTrimmed(src.GenerationExpressionLabel)
	reasoningText := derefTrimmed(src.GenerationExpressionReasoning)

	var buf strings.Builder
	fmt.Fprintf(&buf, `The previous *assistant* message was classified with the expression: %q`, key)
	if t := strings.TrimSpace(labelText); t != "" {
		fmt.Fprintf(&buf, ` (usage hint: %s)`, t)
	}
	if r := strings.TrimSpace(reasoningText); r != "" {
		fmt.Fprintf(&buf, `. Rationale: %s`, r)
	} else {
		fmt.Fprintf(&buf, ".")
	}
	text := buf.String()

	cacheKey := ""
	if src.GenerationExpressionImageID != nil && *src.GenerationExpressionImageID != uuid.Nil {
		cacheKey = userID.String() + "/" + src.GenerationExpressionImageID.String()
	}

	var thumbs []provider.UserMessageImage
	if cacheKey != "" && b.expressionThumbCache != nil {
		raw, mt, ok := b.expressionThumbCache.get(cacheKey)
		if ok && len(raw) > 0 {
			if mt == "" {
				mt = "image/jpeg"
			}
			thumbs = append(thumbs, provider.UserMessageImage{RawBytes: raw, MediaType: mt})
		}
	}

	if len(thumbs) == 0 && src.GenerationExpressionImageID != nil && *src.GenerationExpressionImageID != uuid.Nil && b.fileStore != nil && b.ds != nil {
		att, err := b.ds.GetFileAttachment(ctx, userID, *src.GenerationExpressionImageID)
		if err == nil && att != nil && strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			raw, mt := storage.ResolveAttachmentImageBytes(ctx, b.telemetry.Logger, b.fileStore, userID, att, true)
			if len(raw) > 0 {
				if mt == "" {
					mt = "image/jpeg"
				}
				thumbs = append(thumbs, provider.UserMessageImage{RawBytes: raw, MediaType: mt})
				if cacheKey != "" && b.expressionThumbCache != nil {
					b.expressionThumbCache.put(cacheKey, raw, mt)
				}
			}
		}
	}

	if len(thumbs) > 0 {
		modelCtx.AppendExpressionPortrait(thumbs)
		body := text + provider.ExpressionPortraitContinuityPointerNote
		modelCtx.Append(provider.SegmentKindDeveloperContext, provider.RoleDeveloper, body, false)
		return
	}
	modelCtx.Append(provider.SegmentKindDeveloperContext, provider.RoleDeveloper, text, false)
}

func priorAssistantMessageWithExpression(history []*models.ChatMessage, carryOver [][2]*models.ChatMessage) *models.ChatMessage {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m == nil || m.Origin != models.MessageOriginAssistant {
			continue
		}
		if m.GenerationExpressionKey != nil && strings.TrimSpace(*m.GenerationExpressionKey) != "" {
			return m
		}
	}
	for i := len(carryOver) - 1; i >= 0; i-- {
		a := carryOver[i][1]
		if a == nil {
			continue
		}
		if a.GenerationExpressionKey != nil && strings.TrimSpace(*a.GenerationExpressionKey) != "" {
			return a
		}
	}
	return nil
}

func lastPriorAssistantExpressionSnapshot(history []*models.ChatMessage, carryOver [][2]*models.ChatMessage) (key string, labelText string, reasoningText string, ok bool) {
	m := priorAssistantMessageWithExpression(history, carryOver)
	if m == nil || m.GenerationExpressionKey == nil {
		return "", "", "", false
	}
	return strings.TrimSpace(*m.GenerationExpressionKey), derefTrimmed(m.GenerationExpressionLabel), derefTrimmed(m.GenerationExpressionReasoning), true
}

func derefTrimmed(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func appendItemsFromMessage(msg *models.ChatMessage, out *[]models.AdditionalContextItem) {
	if msg == nil {
		return
	}
	for _, it := range msg.AdditionalContext {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		*out = append(*out, it)
	}
}

// mergeAdditionalContextItems collects typed snippets from checkpoint carry-over, main history,
// the current message row, and this turn's prefetched memories, deduped by type+content.
func mergeAdditionalContextItems(
	carryOver [][2]*models.ChatMessage,
	history []*models.ChatMessage,
	current *models.ChatMessage,
	currentMemories []string,
	currentLive []*models.Memory,
) []models.AdditionalContextItem {
	var raw []models.AdditionalContextItem
	for _, t := range carryOver {
		appendItemsFromMessage(t[0], &raw)
		appendItemsFromMessage(t[1], &raw)
	}
	for _, msg := range history {
		appendItemsFromMessage(msg, &raw)
	}
	appendItemsFromMessage(current, &raw)
	for _, m := range currentMemories {
		if strings.TrimSpace(m) == "" {
			continue
		}
		item := models.AdditionalContextItem{Type: models.AdditionalContextTypeMemory, Content: m}
		if mem := matchLiveMemoryByFormattedContent(m, currentLive); mem != nil {
			id := mem.ID
			item.MemoryID = &id
			item.Scope = normalizeMemoryScope(mem.Scope)
		}
		raw = append(raw, item)
	}
	seen := make(map[string]int, len(raw))
	deduped := make([]models.AdditionalContextItem, 0, len(raw))
	for _, it := range raw {
		scope := normalizeMemoryScope(it.Scope)
		it.Scope = scope
		key := it.Type + "\x00" + scope + "\x00" + additionalContextDedupeIdentity(it)
		if idx, ok := seen[key]; ok {
			// Later entries are fresher: raw is assembled oldest-first (carry-over,
			// history, current row, then this turn's prefetch), so the last rendering of
			// a memory is the one whose age_days and relevance describe *now*. Keep the
			// original position — memory ordering is deliberate — but take the newer
			// rendering. Without this the surviving copy would be the stalest one.
			deduped[idx] = it
			continue
		}
		seen[key] = len(deduped)
		deduped = append(deduped, it)
	}
	return deduped
}

// additionalContextDedupeIdentity returns the part of an additional-context item that
// identifies *which* item it is, independent of how it happened to be rendered.
//
// Memory items cannot be deduped on their rendered content. FormatMemoryForContext
// appends a metadata block — age_days, relevance, reconfirmed — whose values are computed
// at render time, so the same memory rendered on two different days produces two
// different strings. Since prior turns' renderings are persisted on the user message as
// additional_context and replayed alongside a freshly rendered prefetch, keying on the
// raw string let one memory accumulate a new near-duplicate copy per turn, each carrying
// a different age_days for an identical stored_at.
//
// Stripping that block and normalising is the same identity every other memory dedupe
// site in this package uses (see loadedMemoryPoolKey and the merge-inference pass), which
// is what keeps those paths agreeing with this one about what counts as "the same
// memory". MemoryID would be a stronger key but is not populated on every item — the
// synthesised name line has none — and mixing the two would split an id-bearing copy from
// an identical id-less one.
func additionalContextDedupeIdentity(it models.AdditionalContextItem) string {
	if it.Type != models.AdditionalContextTypeMemory {
		return it.Content
	}
	return memoryutil.NormalizeContentForDedupe(memoryutil.StripMemoryContextMetadata(it.Content))
}

func appendMergedAdditionalContext(modelCtx *provider.ModelContext, items []models.AdditionalContextItem) {
	if modelCtx == nil || len(items) == 0 {
		return
	}
	byType := make(map[string][]string)
	memoryRefs := make([]provider.ContextMemoryRef, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		if it.Type == models.AdditionalContextTypeMemory {
			ref := provider.ContextMemoryRef{
				Content: it.Content,
				Scope:   normalizeMemoryScope(it.Scope),
			}
			if it.MemoryID != nil && *it.MemoryID != uuid.Nil {
				ref.MemoryID = it.MemoryID.String()
			}
			if ref.Scope != "" {
				memoryRefs = append(memoryRefs, ref)
			}
		}
		byType[it.Type] = append(byType[it.Type], it.Content)
	}
	if len(memoryRefs) > 0 {
		modelCtx.MemoryRefs = append(modelCtx.MemoryRefs, memoryRefs...)
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	slices.SortFunc(types, func(a, b string) int {
		ao, bo := 1, 1
		if a == models.AdditionalContextTypeMemory {
			ao = 0
		}
		if b == models.AdditionalContextTypeMemory {
			bo = 0
		}
		if ao != bo {
			return ao - bo
		}
		return strings.Compare(a, b)
	})
	for _, typ := range types {
		joined := strings.Join(byType[typ], "\n\n")
		if typ == models.AdditionalContextTypeMemory {
			modelCtx.Append(provider.SegmentKindMemoryContext, provider.RoleDeveloper, joined, false)
		} else {
			modelCtx.Append(provider.SegmentKindDeveloperContext, provider.RoleDeveloper,
				fmt.Sprintf("Additional context (%s):\n\n%s", typ, joined), false)
		}
	}
}

func mergePrompts(prompts ...string) string {
	return strings.Join(prompts, "\n\n")
}
