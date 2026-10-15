package provider

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// WithheldImageNote stands in for an image removed after a provider's safety filter refused a
// request that carried it, so the model still knows an image was there.
const WithheldImageNote = "[An image was here but has been left out: the provider's safety filter refused a request that included it. Do not describe it.]"

// ImageStripper is implemented by agent adapters that can drop the images from the request
// their next Call (or ForceFinalResponse) will send. The agent loop uses it to retry a call the
// provider refused on safety grounds: images, especially freshly generated ones fed back to the
// model, are the likeliest trigger, and the text alone is usually fine.
//
// StripImages replaces each image with WithheldImageNote and returns how many it removed; 0
// means a retry would send the same request. An adapter that keeps earlier rounds on the
// provider's side (OpenAI's previous_response_id) can only strip what it is about to send.
type ImageStripper interface {
	StripImages() int
}

// stripResponsesInputImages removes input_image parts from Responses API input items in place.
func stripResponsesInputImages(items []responses.ResponseInputItemUnionParam) int {
	removed := 0
	for i := range items {
		switch {
		case items[i].OfMessage != nil:
			parts, n := withoutResponsesImages(items[i].OfMessage.Content.OfInputItemContentList)
			if n > 0 {
				items[i].OfMessage.Content.OfInputItemContentList = parts
				removed += n
			}
		case items[i].OfInputMessage != nil:
			parts, n := withoutResponsesImages(items[i].OfInputMessage.Content)
			if n > 0 {
				items[i].OfInputMessage.Content = parts
				removed += n
			}
		}
	}
	return removed
}

func withoutResponsesImages(parts responses.ResponseInputMessageContentListParam) (responses.ResponseInputMessageContentListParam, int) {
	removed := 0
	out := make(responses.ResponseInputMessageContentListParam, 0, len(parts))
	for _, p := range parts {
		if p.OfInputImage != nil {
			removed++
			continue
		}
		out = append(out, p)
	}
	if removed == 0 {
		return parts, 0
	}
	return append(out, responses.ResponseInputContentParamOfInputText(WithheldImageNote)), removed
}

// stripChatCompletionImages removes image_url parts from Chat Completions user messages in place.
func stripChatCompletionImages(messages []openai.ChatCompletionMessageParamUnion) int {
	removed := 0
	for i := range messages {
		user := messages[i].OfUser
		if user == nil || len(user.Content.OfArrayOfContentParts) == 0 {
			continue
		}
		n := 0
		parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(user.Content.OfArrayOfContentParts))
		for _, p := range user.Content.OfArrayOfContentParts {
			if p.OfImageURL != nil {
				n++
				continue
			}
			parts = append(parts, p)
		}
		if n == 0 {
			continue
		}
		user.Content.OfArrayOfContentParts = append(parts, openai.TextContentPart(WithheldImageNote))
		removed += n
	}
	return removed
}

// stripClaudeImages removes image blocks (including those inside tool results) from Messages
// API params in place.
func stripClaudeImages(messages []anthropic.MessageParam) int {
	removed := 0
	for i := range messages {
		n := 0
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(messages[i].Content))
		for _, b := range messages[i].Content {
			if b.OfImage != nil {
				n++
				continue
			}
			if tr := b.OfToolResult; tr != nil {
				kept := tr.Content[:0:0]
				for _, c := range tr.Content {
					if c.OfImage != nil {
						n++
						continue
					}
					kept = append(kept, c)
				}
				tr.Content = kept
			}
			blocks = append(blocks, b)
		}
		if n == 0 {
			continue
		}
		messages[i].Content = append(blocks, anthropic.NewTextBlock(WithheldImageNote))
		removed += n
	}
	return removed
}

// stripClaudeBetaImages is stripClaudeImages for the beta (MCP connector) Messages API.
func stripClaudeBetaImages(messages []anthropic.BetaMessageParam) int {
	removed := 0
	for i := range messages {
		n := 0
		blocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(messages[i].Content))
		for _, b := range messages[i].Content {
			if b.OfImage != nil {
				n++
				continue
			}
			if tr := b.OfToolResult; tr != nil {
				kept := tr.Content[:0:0]
				for _, c := range tr.Content {
					if c.OfImage != nil {
						n++
						continue
					}
					kept = append(kept, c)
				}
				tr.Content = kept
			}
			blocks = append(blocks, b)
		}
		if n == 0 {
			continue
		}
		messages[i].Content = append(blocks, anthropic.NewBetaTextBlock(WithheldImageNote))
		removed += n
	}
	return removed
}

// StripImages implements ImageStripper. Earlier rounds live on OpenAI's side behind
// previous_response_id, so only the input this adapter is about to send can be stripped: the
// user's images on the first round, or the generated images staged with tool results after.
func (a *OpenAIAdapter) StripImages() int {
	return stripResponsesInputImages(a.params.Input.OfInputItemList)
}

// StripImages implements ImageStripper over the whole message list the next call re-sends.
func (a *ClaudeAdapter) StripImages() int {
	if a.useBetaMCP {
		return stripClaudeBetaImages(a.betaParams.Messages)
	}
	return stripClaudeImages(a.params.Messages)
}

// StripImages implements ImageStripper.
func (a *GeminiAdapter) StripImages() int { return stripChatCompletionImages(a.params.Messages) }

// StripImages implements ImageStripper.
func (a *QwenAdapter) StripImages() int { return stripChatCompletionImages(a.params.Messages) }

// StripImages implements ImageStripper.
func (a *MistralAdapter) StripImages() int { return stripChatCompletionImages(a.params.Messages) }

// StripImages implements ImageStripper.
func (a *DeepSeekAdapter) StripImages() int { return stripChatCompletionImages(a.params.Messages) }

// StripImages implements ImageStripper.
func (a *XiaomiAdapter) StripImages() int { return stripChatCompletionImages(a.params.Messages) }

// StripImages implements ImageStripper.
func (a *LocalAdapter) StripImages() int { return stripChatCompletionImages(a.params.Messages) }
