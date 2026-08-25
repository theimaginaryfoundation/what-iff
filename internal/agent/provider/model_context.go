// Package provider maps ModelContext segments to OpenAI Responses and Anthropic Messages
// request shapes.
//
// Anthropic vision constraint: image blocks are only permitted on user turns. Images on
// assistant messages produce 400 invalid_request_error ("'image' blocks are not permitted
// within assistant turns"). Historical assistant output that includes images (e.g.
// generate_image attachments) must be split: assistant text, then a following user
// message carrying the images. OpenAI Responses has the same shape requirement for a
// different reason (assistant turns reject input_text). See renderClaudeContext,
// appendOpenAIHistoryTurn, and HistoryAssistantImageCaption.
//
// Anthropic server-tool constraint: web_search_tool_result blocks carry encrypted_content
// for multi-turn continuity. Do not use full Message.ToParam() during the in-turn agent
// loop — it can mangle content on resubmit (400 invalid_request_error). Instead
// appendClaudeAssistantLoopTurn replays blocks individually via block.ToParam(), preserving
// encrypted_content for Anthropic server-side decryption. Cross-turn context still uses
// formatted text ToolCalls (appendPersistedToolResults). Tool-produced images in the same
// loop are injected via AppendToolResults (GeneratedToolImageCaption user turn).
package provider

import (
	"slices"
	"strings"
)

type ModelContextSegmentKind string

const (
	SegmentKindSystemPrompt       ModelContextSegmentKind = "system_prompt"
	SegmentKindScratchpad         ModelContextSegmentKind = "scratchpad"
	SegmentKindCheckpointSummary  ModelContextSegmentKind = "checkpoint_summary"
	SegmentKindHistoryTurn        ModelContextSegmentKind = "history_turn"
	SegmentKindMemoryContext      ModelContextSegmentKind = "memory_context"
	SegmentKindAttachmentContext  ModelContextSegmentKind = "attachment_context"
	SegmentKindToolResult         ModelContextSegmentKind = "tool_result"
	SegmentKindMood               ModelContextSegmentKind = "mood"
	SegmentKindDeveloperContext   ModelContextSegmentKind = "developer_context"
	SegmentKindExpressionPortrait ModelContextSegmentKind = "expression_portrait"
	SegmentKindUserMessage        ModelContextSegmentKind = "user_message"
	// SegmentKindToolDefinitions represents request tool schemas. They are sent outside
	// the conversational message list, so their token estimate is recorded separately.
	SegmentKindToolDefinitions ModelContextSegmentKind = "tool_definitions"
)

// UserMessageImage is one image payload for multimodal turns. FileID is the OpenAI Files
// API id (Responses input_image). RawBytes is used for Claude vision and OpenAI data-URL
// fallback when FileID is absent.
//
// Anthropic: image blocks may only appear on user turns — never attach UserImages to an
// assistant history segment without splitting (see package doc above).
type UserMessageImage struct {
	FileID    string
	MediaType string
	RawBytes  []byte
}

// ModelContextSegment is a single unit of model context.
//
// Clone assumption: all fields except UserImages are value types (string, bool,
// a string-alias kind). UserImages is the only slice field and is explicitly
// deep-copied by Clone. If a new reference-type field (slice, map, pointer) is
// added here, Clone must be updated to deep-copy it as well.
type ModelContextSegment struct {
	Kind      ModelContextSegmentKind
	Role      string
	Content   string
	Cacheable bool
	// UserImages is set only for SegmentKindUserMessage when the user attached images.
	UserImages []UserMessageImage
}

// ModelContext is an ordered, provider-agnostic representation of model context.
// Provider-specific renderers map segments into SDK request types.
//
// Caching convention (important):
//   - Cacheable segments are intended to appear first as a contiguous prefix.
//   - Non-cacheable segments should follow after that prefix.
//   - This is documented behavior for now; ordering is not strictly enforced yet.
//
// This convention allows provider renderers (especially Claude prompt caching) to
// apply cache controls deterministically without reconstructing segment order.
type ModelContext struct {
	Segments                 []ModelContextSegment
	MemoryRefs               []ContextMemoryRef
	AdditionalTokenEstimates map[ModelContextSegmentKind]int
}

// ContextMemoryRef tracks structured memory snippets backing SegmentKindMemoryContext.
type ContextMemoryRef struct {
	MemoryID string
	Content  string
	Scope    string
}

func (m *ModelContext) Append(kind ModelContextSegmentKind, role, content string, cacheable bool) {
	if m == nil || content == "" {
		return
	}
	m.Segments = append(m.Segments, ModelContextSegment{
		Kind:      kind,
		Role:      role,
		Content:   content,
		Cacheable: cacheable,
	})
}

// AppendHistoryTurn appends a prior conversation turn, optionally with image attachments
// for vision continuity across turns.
func (m *ModelContext) AppendHistoryTurn(role, content string, images []UserMessageImage, cacheable bool) {
	if m == nil {
		return
	}
	if strings.TrimSpace(content) == "" && len(images) == 0 {
		return
	}
	imgs := append([]UserMessageImage(nil), images...)
	m.Segments = append(m.Segments, ModelContextSegment{
		Kind:       SegmentKindHistoryTurn,
		Role:       role,
		Content:    content,
		Cacheable:  cacheable,
		UserImages: imgs,
	})
}

// AppendUserMessage appends the final user turn, optionally with images (OpenAI file ids + media types).
// Allows empty text when at least one image is present.
func (m *ModelContext) AppendUserMessage(role, content string, images []UserMessageImage, cacheable bool) {
	if m == nil {
		return
	}
	if strings.TrimSpace(content) == "" && len(images) == 0 {
		return
	}
	imgs := append([]UserMessageImage(nil), images...)
	m.Segments = append(m.Segments, ModelContextSegment{
		Kind:       SegmentKindUserMessage,
		Role:       role,
		Content:    content,
		Cacheable:  cacheable,
		UserImages: imgs,
	})
}

// AppendExpressionPortrait appends a synthetic user turn carrying the prior-turn expression
// portrait. It is placed immediately before developer continuity text and the real user
// message so providers do not merge the portrait into the current user turn.
func (m *ModelContext) AppendExpressionPortrait(images []UserMessageImage) {
	if m == nil || len(images) == 0 {
		return
	}
	m.Segments = append(m.Segments, ModelContextSegment{
		Kind:       SegmentKindExpressionPortrait,
		Role:       RoleUser,
		Content:    ExpressionPortraitVisualReferenceCaption,
		Cacheable:  false,
		UserImages: append([]UserMessageImage(nil), images...),
	})
}

// InsertBeforeLastUserMessage inserts a segment immediately before the last
// SegmentKindUserMessage segment (the current turn). If there is no user
// message segment, it appends. Used so developer-only context (e.g. extra
// instructions) sits directly before the final user message, not after it.
func (m *ModelContext) InsertBeforeLastUserMessage(kind ModelContextSegmentKind, role, content string, cacheable bool) {
	if m == nil || content == "" {
		return
	}
	seg := ModelContextSegment{
		Kind:      kind,
		Role:      role,
		Content:   content,
		Cacheable: cacheable,
	}
	for i := len(m.Segments) - 1; i >= 0; i-- {
		if m.Segments[i].Kind == SegmentKindUserMessage {
			m.Segments = slices.Insert(m.Segments, i, seg)
			return
		}
	}
	m.Segments = append(m.Segments, seg)
}

// SetAdditionalTokenEstimate records a token estimate for request material that is
// not rendered as a conversational segment, such as function-tool definitions.
func (m *ModelContext) SetAdditionalTokenEstimate(kind ModelContextSegmentKind, tokens int) {
	if m == nil || tokens <= 0 {
		return
	}
	if m.AdditionalTokenEstimates == nil {
		m.AdditionalTokenEstimates = make(map[ModelContextSegmentKind]int)
	}
	m.AdditionalTokenEstimates[kind] = tokens
}

// Clone returns an independent copy of the ModelContext. The Segments slice is
// freshly allocated, so Append calls on the clone do not affect the original.
// Each segment's value fields are copied directly (see ModelContextSegment for
// the clone-safety contract); UserImages is the only reference field and is
// explicitly deep-copied here.
func (m *ModelContext) Clone() *ModelContext {
	if m == nil {
		return nil
	}
	segs := make([]ModelContextSegment, len(m.Segments))
	for i, s := range m.Segments {
		segs[i] = s
		if len(s.UserImages) > 0 {
			segs[i].UserImages = append([]UserMessageImage(nil), s.UserImages...)
		}
	}
	refs := make([]ContextMemoryRef, len(m.MemoryRefs))
	copy(refs, m.MemoryRefs)
	var additional map[ModelContextSegmentKind]int
	if m.AdditionalTokenEstimates != nil {
		additional = make(map[ModelContextSegmentKind]int, len(m.AdditionalTokenEstimates))
		for kind, tokens := range m.AdditionalTokenEstimates {
			additional[kind] = tokens
		}
	}
	return &ModelContext{Segments: segs, MemoryRefs: refs, AdditionalTokenEstimates: additional}
}

// StripUserMessageImages clears image payloads on every segment. Use when reusing a
// ModelContext for an API that only accepts text (e.g. expression picker + strict JSON
// schema output, where OpenAI returns 400 if any message content includes input_image).
//
// Invariants after stripping:
//   - SegmentKindExpressionPortrait segments are removed entirely (caption + image are a unit).
//   - SegmentKindUserMessage turns that were image-only (no text) are removed.
func (m *ModelContext) StripUserMessageImages() {
	if m == nil {
		return
	}
	write := 0
	for read := 0; read < len(m.Segments); read++ {
		seg := m.Segments[read]
		if seg.Kind == SegmentKindExpressionPortrait {
			continue
		}
		if len(seg.UserImages) > 0 {
			seg.UserImages = nil
			if seg.Kind == SegmentKindUserMessage && strings.TrimSpace(seg.Content) == "" {
				continue
			}
		}
		m.Segments[write] = seg
		write++
	}
	m.Segments = m.Segments[:write]
}

// PrepareForTextOnlyChatCompletions strips multimodal payloads for OpenAI-compatible Chat
// Completions providers without vision (DeepSeek, MiMo; non-vision Qwen/Mistral ids).
// Gemini uses a separate call path; vision-capable Qwen/Mistral models skip this helper.
//
// Invariants after preparation:
//   - SegmentKindExpressionPortrait segments are removed entirely.
//   - Image payloads are cleared on all remaining segments.
//   - Image-only SegmentKindUserMessage turns become TextOnlyChatCompletionsImageFallback.
func (m *ModelContext) PrepareForTextOnlyChatCompletions() {
	if m == nil {
		return
	}
	write := 0
	for read := 0; read < len(m.Segments); read++ {
		seg := m.Segments[read]
		if seg.Kind == SegmentKindExpressionPortrait {
			continue
		}
		if len(seg.UserImages) > 0 {
			seg.UserImages = nil
			if seg.Kind == SegmentKindUserMessage && strings.TrimSpace(seg.Content) == "" {
				seg.Content = TextOnlyChatCompletionsImageFallback
			}
		}
		m.Segments[write] = seg
		write++
	}
	m.Segments = m.Segments[:write]
}

func (m *ModelContext) SystemPrompt() string {
	if m == nil {
		return ""
	}
	for _, s := range m.Segments {
		if s.Kind == SegmentKindSystemPrompt {
			return s.Content
		}
	}
	return ""
}

// EstimatedTokensBySegment sums TokenCounter estimates per segment kind (multiple
// segments of the same kind are aggregated).
func (m *ModelContext) EstimatedTokensBySegment(counter *TokenCounter) map[ModelContextSegmentKind]int {
	if m == nil || counter == nil {
		return nil
	}
	out := make(map[ModelContextSegmentKind]int)
	for _, seg := range m.Segments {
		if seg.Content == "" {
			continue
		}
		n, err := counter.CountTokens(seg.Content)
		if err != nil {
			continue
		}
		out[seg.Kind] += n
	}
	return out
}

// SegmentKindStat is a per-kind rollup of the context segments: how many segments of a
// kind are present, their aggregated estimated token count, and whether any were marked
// cacheable (part of the prompt-caching prefix). Provider-neutral by design so the agent
// layer can map it into an API DTO without this package importing internal/models.
type SegmentKindStat struct {
	Kind      ModelContextSegmentKind
	Segments  int
	Tokens    int
	Cacheable bool
	// Images is the number of image payloads carried by segments of this kind
	// (e.g. user-attached images, expression portraits) — token estimates do not
	// account for these, so surfacing the count keeps the breakdown honest.
	Images int
}

// SegmentBreakdown rolls the ordered segments up by kind, preserving first-appearance
// order so the result reads top-to-bottom the way the context is actually laid out for
// the model. Empty-content segments still count toward Segments/Images (an image-only
// user turn has no text but is real context); their token contribution is just zero.
func (m *ModelContext) SegmentBreakdown(counter *TokenCounter) []SegmentKindStat {
	if m == nil {
		return nil
	}
	index := make(map[ModelContextSegmentKind]int, len(m.Segments))
	stats := make([]SegmentKindStat, 0, len(m.Segments))
	for _, seg := range m.Segments {
		i, ok := index[seg.Kind]
		if !ok {
			i = len(stats)
			index[seg.Kind] = i
			stats = append(stats, SegmentKindStat{Kind: seg.Kind})
		}
		stats[i].Segments++
		stats[i].Images += len(seg.UserImages)
		if seg.Cacheable {
			stats[i].Cacheable = true
		}
		if seg.Content != "" && counter != nil {
			if n, err := counter.CountTokens(seg.Content); err == nil {
				stats[i].Tokens += n
			}
		}
	}
	for kind, tokens := range m.AdditionalTokenEstimates {
		if tokens <= 0 {
			continue
		}
		stats = append(stats, SegmentKindStat{
			Kind:     kind,
			Segments: 1,
			Tokens:   tokens,
		})
	}
	return stats
}
