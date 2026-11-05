package provider

import (
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
		if len(im.RawBytes) == 0 {
			continue
		}
		dataURL := imageDataURL(normalizeImageMediaType(im.MediaType), im.RawBytes)
		img := &responses.ResponseInputImageParam{
			Detail:   responses.ResponseInputImageDetailAuto,
			ImageURL: openai.String(dataURL),
		}
		parts = append(parts, responses.ResponseInputContentUnionParam{OfInputImage: img})
	}
	return parts
}

// openAISupportedImageMediaTypes are the image media types the OpenAI Responses API
// accepts as input. Other vendors (Anthropic, Gemini) accept a broader set, so an image
// that arrived on a Claude/Gemini turn can carry a media type OpenAI rejects with a 4xx.
var openAISupportedImageMediaTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/jpg":  {}, // non-standard, but some clients send it
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

// openAIAcceptsImage reports whether the OpenAI Responses API will accept this image.
//
// OpenAI rejects an input image with an unsupported media type (e.g. image/heic that rode in
// on another vendor's turn). An image is only accepted when it carries raw bytes to render as
// a data URL, whose media type we control via normalizeImageMediaType and check against the
// supported list here.
func openAIAcceptsImage(im UserMessageImage) bool {
	if len(im.RawBytes) == 0 {
		return false
	}
	mt := strings.ToLower(strings.TrimSpace(im.MediaType))
	if mt == "" {
		mt = "image/jpeg" // matches normalizeImageMediaType's default for the data-URL path
	}
	_, ok := openAISupportedImageMediaTypes[mt]
	return ok
}

// OpenAIRejectedImages counts the images SanitizeImagesForOpenAIInput drops from ctx: those
// with no bytes to send inline, and those of a media type the Responses API rejects.
func OpenAIRejectedImages(ctx *ModelContext) (noData, unsupportedType int) {
	if ctx == nil {
		return 0, 0
	}
	for _, seg := range ctx.Segments {
		for _, im := range seg.UserImages {
			switch {
			case len(im.RawBytes) == 0:
				noData++
			case !openAIAcceptsImage(im):
				unsupportedType++
			}
		}
	}
	return noData, unsupportedType
}

// SanitizeImagesForOpenAIInput returns a clone of ctx whose image attachments are safe to
// send to the OpenAI Responses API. Images OpenAI would reject (see openAIAcceptsImage) are
// dropped. A segment left with no images and no text is removed, mirroring
// StripUserMessageImages, so we never emit an empty turn. Intended for background paths like
// the checkpoint summarizer, where one bad image must not fail the whole request.
func SanitizeImagesForOpenAIInput(ctx *ModelContext) *ModelContext {
	if ctx == nil {
		return nil
	}
	clone := ctx.Clone()
	write := 0
	for read := 0; read < len(clone.Segments); read++ {
		seg := clone.Segments[read]
		if len(seg.UserImages) > 0 {
			kept := seg.UserImages[:0]
			for _, im := range seg.UserImages {
				if !openAIAcceptsImage(im) {
					continue
				}
				kept = append(kept, im)
			}
			if len(kept) == 0 {
				seg.UserImages = nil
				// Drop turns that were image-only once their images are gone.
				if strings.TrimSpace(seg.Content) == "" {
					continue
				}
			} else {
				seg.UserImages = kept
			}
		}
		clone.Segments[write] = seg
		write++
	}
	clone.Segments = clone.Segments[:write]
	return clone
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
		case SegmentKindMood, SegmentKindSkills, SegmentKindSkillsIndex:
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
