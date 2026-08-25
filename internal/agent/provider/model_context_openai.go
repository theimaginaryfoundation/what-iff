package provider

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// OpenAIResponseParamsOptions configures BuildOpenAIResponseParams for the Responses API.
// Instructions: if empty, SystemPrompt() from the ModelContext is used.
type OpenAIResponseParamsOptions struct {
	Model             string
	SafetyUserID      string
	MaxOutputTokens   int
	ParallelToolCalls bool
	Tools             []responses.ToolUnionParam
	Instructions      string
	// Include requests extra fields on tool call items (e.g. web_search_call.results).
	Include []responses.ResponseIncludable
}

// BuildOpenAIResponseParams builds responses.ResponseNewParams from this context, mirroring
// BuildClaudeParams for the Anthropic path.
func (m *ModelContext) BuildOpenAIResponseParams(opts OpenAIResponseParamsOptions) responses.ResponseNewParams {

	maxOut := opts.MaxOutputTokens
	if maxOut == 0 {
		maxOut = DefaultMaxContentLength
	}
	instr := opts.Instructions
	if instr == "" && m != nil {
		instr = m.SystemPrompt()
	}
	params := responses.ResponseNewParams{
		Model:            opts.Model,
		SafetyIdentifier: openai.String(opts.SafetyUserID),
		MaxOutputTokens:  openai.Int(int64(maxOut)),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: RenderOpenAIInputItems(m),
		},
		Instructions: openai.String(instr),
	}
	if opts.ParallelToolCalls {
		params.ParallelToolCalls = openai.Bool(true)
	}
	if len(opts.Tools) > 0 {
		params.Tools = opts.Tools
	}
	if len(opts.Include) > 0 {
		params.Include = opts.Include
	}
	return params
}

func appendOpenAIResponseInputImages(parts responses.ResponseInputMessageContentListParam, images []UserMessageImage) responses.ResponseInputMessageContentListParam {
	for _, im := range images {
		img := &responses.ResponseInputImageParam{
			Detail: responses.ResponseInputImageDetailAuto,
		}
		if strings.TrimSpace(im.FileID) != "" {
			img.FileID = openai.String(im.FileID)
		} else if len(im.RawBytes) > 0 {
			dataURL := fmt.Sprintf("data:%s;base64,%s", normalizeImageMediaType(im.MediaType), base64.StdEncoding.EncodeToString(im.RawBytes))
			img.ImageURL = openai.String(dataURL)
		} else {
			continue
		}
		parts = append(parts, responses.ResponseInputContentUnionParam{OfInputImage: img})
	}
	return parts
}

func normalizeImageMediaType(mediaType string) string {
	mt := strings.TrimSpace(mediaType)
	if mt == "" || !strings.HasPrefix(strings.ToLower(mt), "image/") {
		return "image/jpeg"
	}
	return mt
}

// appendOpenAIHistoryTurn renders a prior conversation turn. Assistant turns with images
// must split text (output_text on role assistant) from images (input_text/input_image on
// role user) — the Responses API rejects input_text on assistant messages.
func appendOpenAIHistoryTurn(out *[]responses.ResponseInputItemUnionParam, seg ModelContextSegment) {
	if len(seg.UserImages) == 0 {
		*out = append(*out, responses.ResponseInputItemParamOfMessage(seg.Content, responses.EasyInputMessageRole(seg.Role)))
		return
	}
	role := seg.Role
	if role == "" {
		role = RoleUser
	}
	if role == RoleAssistant {
		if strings.TrimSpace(seg.Content) != "" {
			*out = append(*out, responses.ResponseInputItemParamOfMessage(seg.Content, RoleAssistant))
		}
		var parts responses.ResponseInputMessageContentListParam
		parts = append(parts, responses.ResponseInputContentParamOfInputText(HistoryAssistantImageCaption))
		parts = appendOpenAIResponseInputImages(parts, seg.UserImages)
		*out = append(*out, responses.ResponseInputItemParamOfMessage(parts, RoleUser))
		return
	}
	var parts responses.ResponseInputMessageContentListParam
	if strings.TrimSpace(seg.Content) != "" {
		parts = append(parts, responses.ResponseInputContentParamOfInputText(seg.Content))
	}
	parts = appendOpenAIResponseInputImages(parts, seg.UserImages)
	*out = append(*out, responses.ResponseInputItemParamOfMessage(parts, RoleUser))
}

func RenderOpenAIInputItems(ctx *ModelContext) []responses.ResponseInputItemUnionParam {
	if ctx == nil {
		return nil
	}
	out := make([]responses.ResponseInputItemUnionParam, 0, len(ctx.Segments))
	for _, seg := range ctx.Segments {
		switch seg.Kind {
		case SegmentKindSystemPrompt:
			// Rendered into ResponseNewParams.Instructions by caller.
		case SegmentKindScratchpad:
			out = append(out, responses.ResponseInputItemParamOfMessage(
				fmt.Sprintf("Current Scratchpad contents:\n%s", seg.Content),
				RoleDeveloper,
			))
		case SegmentKindCheckpointSummary:
			out = append(out, responses.ResponseInputItemParamOfMessage(
				fmt.Sprintf("Conversation checkpoint summary:\n%s", seg.Content),
				RoleDeveloper,
			))
		case SegmentKindHistoryTurn:
			appendOpenAIHistoryTurn(&out, seg)
		case SegmentKindExpressionPortrait:
			var parts responses.ResponseInputMessageContentListParam
			if strings.TrimSpace(seg.Content) != "" {
				parts = append(parts, responses.ResponseInputContentParamOfInputText(seg.Content))
			}
			parts = appendOpenAIResponseInputImages(parts, seg.UserImages)
			out = append(out, responses.ResponseInputItemParamOfMessage(parts, RoleUser))
		case SegmentKindMemoryContext:
			out = append(out, responses.ResponseInputItemParamOfMessage(
				fmt.Sprintf("Here are potentially relevant memories:\n\n %s", seg.Content),
				RoleDeveloper,
			))
		case SegmentKindAttachmentContext:
			out = append(out, responses.ResponseInputItemParamOfMessage(seg.Content, RoleDeveloper))
		case SegmentKindToolResult:
			out = append(out, responses.ResponseInputItemParamOfMessage(seg.Content, RoleDeveloper))
		case SegmentKindMood:
			out = append(out, responses.ResponseInputItemParamOfMessage(seg.Content, RoleDeveloper))
		case SegmentKindDeveloperContext:
			out = append(out, responses.ResponseInputItemParamOfMessage(seg.Content, RoleDeveloper))
		case SegmentKindUserMessage:
			msgRole := seg.Role
			if msgRole == "" {
				msgRole = RoleUser
			}
			if len(seg.UserImages) == 0 {
				out = append(out, responses.ResponseInputItemParamOfMessage(seg.Content, responses.EasyInputMessageRole(msgRole)))
				break
			}
			var parts responses.ResponseInputMessageContentListParam
			if strings.TrimSpace(seg.Content) != "" {
				parts = append(parts, responses.ResponseInputContentParamOfInputText(seg.Content))
			}
			parts = appendOpenAIResponseInputImages(parts, seg.UserImages)
			out = append(out, responses.ResponseInputItemParamOfMessage(parts, responses.EasyInputMessageRole(msgRole)))
		}
	}
	return out
}
