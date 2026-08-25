package provider

import (
	"encoding/base64"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

func claudeEphemeralCacheControl5m() anthropic.CacheControlEphemeralParam {
	cc := anthropic.NewCacheControlEphemeralParam()
	cc.TTL = anthropic.CacheControlEphemeralTTLTTL5m
	return cc
}

// lastContiguousCacheableSegmentIndex returns the last segment index in the
// cacheable prefix from the start of Segments (first run of Cacheable==true).
// Returns -1 when the first segment is not cacheable or Segments is empty.
func lastContiguousCacheableSegmentIndex(segs []ModelContextSegment) int {
	last := -1
	for i := range segs {
		if !segs[i].Cacheable {
			break
		}
		last = i
	}
	return last
}

func textBlockUnionWithOptionalCache(text string, markCache bool) anthropic.ContentBlockParamUnion {
	u := anthropic.NewTextBlock(text)
	if markCache && u.OfText != nil {
		u.OfText.CacheControl = claudeEphemeralCacheControl5m()
	}
	return u
}

func claudeUserMessageBlocksWithCaptionFirst(seg ModelContextSegment, markCache bool) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(seg.UserImages)+1)
	if strings.TrimSpace(seg.Content) != "" {
		blocks = append(blocks, textBlockUnionWithOptionalCache(seg.Content, markCache))
	}
	for _, im := range seg.UserImages {
		if len(im.RawBytes) == 0 {
			continue
		}
		mt := im.MediaType
		if strings.TrimSpace(mt) == "" {
			mt = "image/jpeg"
		}
		enc := base64.StdEncoding.EncodeToString(im.RawBytes)
		blocks = append(blocks, anthropic.NewImageBlockBase64(mt, enc))
	}
	return blocks
}

func renderClaudeContext(ctx *ModelContext) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	if ctx == nil {
		return nil, nil
	}
	lastCC := lastContiguousCacheableSegmentIndex(ctx.Segments)
	systemBlocks := make([]anthropic.TextBlockParam, 0, 3)
	messages := make([]anthropic.MessageParam, 0, len(ctx.Segments))

	for i, seg := range ctx.Segments {
		mark := i == lastCC && lastCC >= 0
		switch seg.Kind {
		case SegmentKindSystemPrompt:
			tb := anthropic.TextBlockParam{Text: seg.Content}
			if mark {
				tb.CacheControl = claudeEphemeralCacheControl5m()
			}
			systemBlocks = append(systemBlocks, tb)
		case SegmentKindScratchpad:
			tb := anthropic.TextBlockParam{Text: "Current Scratchpad contents:\n" + seg.Content}
			if mark {
				tb.CacheControl = claudeEphemeralCacheControl5m()
			}
			systemBlocks = append(systemBlocks, tb)
		case SegmentKindCheckpointSummary:
			tb := anthropic.TextBlockParam{Text: "Conversation checkpoint summary:\n" + seg.Content}
			if mark {
				tb.CacheControl = claudeEphemeralCacheControl5m()
			}
			systemBlocks = append(systemBlocks, tb)
		case SegmentKindHistoryTurn:
			appendClaudeHistoryTurn(&messages, seg, mark)
		case SegmentKindExpressionPortrait:
			blocks := claudeUserMessageBlocksWithCaptionFirst(seg, mark)
			if len(blocks) == 0 {
				break
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
		case SegmentKindMemoryContext:
			messages = append(messages,
				anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf("[CONTEXT] Here are potentially relevant memories:\n\n%s", seg.Content))),
				anthropic.NewAssistantMessage(anthropic.NewTextBlock("Understood. I have the memories in context.")),
			)
		case SegmentKindUserMessage:
			if len(seg.UserImages) == 0 {
				messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
				break
			}
			allImageBytes := true
			for _, im := range seg.UserImages {
				if len(im.RawBytes) == 0 {
					allImageBytes = false
					break
				}
			}
			if !allImageBytes {
				// No raw bytes (e.g. only OpenAI file_id): vision not available on Claude until S3 or similar.
				messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
				break
			}
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(seg.UserImages)+1)
			for _, im := range seg.UserImages {
				enc := base64.StdEncoding.EncodeToString(im.RawBytes)
				mt := im.MediaType
				if strings.TrimSpace(mt) == "" {
					mt = "image/jpeg"
				}
				blocks = append(blocks, anthropic.NewImageBlockBase64(mt, enc))
			}
			if strings.TrimSpace(seg.Content) != "" {
				blocks = append(blocks, textBlockUnionWithOptionalCache(seg.Content, mark))
			}
			if len(blocks) == 0 {
				messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
				break
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
		case SegmentKindDeveloperContext:
			messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
		case SegmentKindMood:
			messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
		case SegmentKindToolResult:
			messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
		case SegmentKindAttachmentContext:
			messages = append(messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, mark)))
		}
	}

	return systemBlocks, messages
}

func claudeImageBlocksFromUserImages(images []UserMessageImage, leadingText string) []anthropic.ContentBlockParamUnion {
	var imageBlocks []anthropic.ContentBlockParamUnion
	for _, im := range images {
		if len(im.RawBytes) == 0 {
			continue
		}
		mt := im.MediaType
		if strings.TrimSpace(mt) == "" {
			mt = "image/jpeg"
		}
		imageBlocks = append(imageBlocks, anthropic.NewImageBlockBase64(mt, base64.StdEncoding.EncodeToString(im.RawBytes)))
	}
	if len(imageBlocks) == 0 {
		return nil
	}
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(imageBlocks)+1)
	if t := strings.TrimSpace(leadingText); t != "" {
		blocks = append(blocks, anthropic.NewTextBlock(t))
	}
	return append(blocks, imageBlocks...)
}

func claudeHistoryTurnHasImageBytes(images []UserMessageImage) bool {
	for _, im := range images {
		if len(im.RawBytes) > 0 {
			return true
		}
	}
	return false
}

// appendClaudeHistoryTurn renders a prior conversation turn for Anthropic. Assistant
// turns with images must split text and vision: Anthropic rejects image blocks on
// assistant messages (see package doc on UserMessageImage).
func appendClaudeHistoryTurn(messages *[]anthropic.MessageParam, seg ModelContextSegment, markCache bool) {
	if len(seg.UserImages) == 0 || !claudeHistoryTurnHasImageBytes(seg.UserImages) {
		if seg.Role == RoleUser {
			*messages = append(*messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, markCache)))
		} else if seg.Role == RoleAssistant {
			*messages = append(*messages, anthropic.NewAssistantMessage(textBlockUnionWithOptionalCache(seg.Content, markCache)))
		}
		return
	}

	if seg.Role == RoleAssistant {
		if strings.TrimSpace(seg.Content) != "" {
			*messages = append(*messages, anthropic.NewAssistantMessage(textBlockUnionWithOptionalCache(seg.Content, markCache)))
		}
		blocks := claudeImageBlocksFromUserImages(seg.UserImages, HistoryAssistantImageCaption)
		if len(blocks) > 0 {
			*messages = append(*messages, anthropic.NewUserMessage(blocks...))
		}
		return
	}

	blocks := claudeImageBlocksFromUserImages(seg.UserImages, "")
	if strings.TrimSpace(seg.Content) != "" {
		blocks = append(blocks, textBlockUnionWithOptionalCache(seg.Content, markCache))
	}
	if len(blocks) == 0 {
		*messages = append(*messages, anthropic.NewUserMessage(textBlockUnionWithOptionalCache(seg.Content, markCache)))
		return
	}
	*messages = append(*messages, anthropic.NewUserMessage(blocks...))
}

func (m *ModelContext) BuildClaudeParams(model string) anthropic.MessageNewParams {

	systemBlocks, messages := renderClaudeContext(m)

	return anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(DefaultMaxContentLength),
		System:    systemBlocks,
		Messages:  messages,
	}
}
