package provider

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// renderGeminiMessages maps ModelContext segments into Chat Completions messages.
//
// Gemini's OpenAI-compatible endpoint has no "developer" role and does not support
// the Responses API's input-item shapes, so this renderer is intentionally simpler
// than the OpenAI Responses renderer: system-ish segments become system messages and
// everything conversational becomes user/assistant turns.
func renderGeminiMessages(ctx *ModelContext) []openai.ChatCompletionMessageParamUnion {
	if ctx == nil {
		return nil
	}
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(ctx.Segments))
	for _, seg := range ctx.Segments {
		switch seg.Kind {
		case SegmentKindSystemPrompt:
			messages = append(messages, openai.SystemMessage(seg.Content))
		case SegmentKindScratchpad:
			messages = append(messages, openai.SystemMessage("Current Scratchpad contents:\n"+seg.Content))
		case SegmentKindCheckpointSummary:
			messages = append(messages, openai.SystemMessage("Conversation checkpoint summary:\n"+seg.Content))
		case SegmentKindHistoryTurn:
			if seg.Role == RoleAssistant {
				messages = append(messages, openai.AssistantMessage(seg.Content))
			} else {
				messages = append(messages, openai.UserMessage(seg.Content))
			}
		case SegmentKindMemoryContext:
			messages = append(messages,
				openai.UserMessage(fmt.Sprintf("[CONTEXT] Here are potentially relevant memories:\n\n%s", seg.Content)),
				openai.AssistantMessage("Understood. I have the memories in context."),
			)
		case SegmentKindUserMessage, SegmentKindExpressionPortrait:
			appendGeminiUserTurn(&messages, seg.Content, seg.UserImages)
		case SegmentKindDeveloperContext, SegmentKindMood, SegmentKindAttachmentContext, SegmentKindToolResult:
			if strings.TrimSpace(seg.Content) == "" {
				break
			}
			messages = append(messages, openai.UserMessage(seg.Content))
		}
	}
	return messages
}

// appendGeminiUserTurn maps a user-role segment (final user message or expression portrait)
// into Chat Completions user messages, including multimodal parts when RawBytes are set.
func appendGeminiUserTurn(messages *[]openai.ChatCompletionMessageParamUnion, text string, images []UserMessageImage) {
	parts := geminiUserMessageParts(text, images)
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 && parts[0].OfText != nil && !param.IsOmitted(parts[0].OfText) {
		*messages = append(*messages, openai.UserMessage(parts[0].OfText.Text))
		return
	}
	*messages = append(*messages, openai.UserMessage(parts))
}

// geminiUserMessageParts builds Chat Completions content parts for a user turn.
// Images are sent as base64 data URLs when RawBytes is populated (same prefetch path
// as Claude vision); otherwise the turn is text-only.
func geminiUserMessageParts(text string, images []UserMessageImage) []openai.ChatCompletionContentPartUnionParam {
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, openai.TextContentPart(text))
	}
	for _, im := range images {
		if len(im.RawBytes) == 0 {
			continue
		}
		mt := im.MediaType
		if strings.TrimSpace(mt) == "" {
			mt = "image/jpeg"
		}
		url := fmt.Sprintf("data:%s;base64,%s", mt, base64.StdEncoding.EncodeToString(im.RawBytes))
		parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL: url,
		}))
	}
	return parts
}

// BuildOpenAIChatCompletionParams builds Chat Completions params from the model context.
// Shared by Gemini and other OpenAI-compatible Chat Completions providers (Mistral, DeepSeek, Qwen, Xiaomi).
func (m *ModelContext) BuildOpenAIChatCompletionParams(model string) openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(model),
		Messages:            renderGeminiMessages(m),
		MaxCompletionTokens: openai.Int(int64(DefaultMaxContentLength)),
	}
}

// BuildGeminiParams is an alias for BuildOpenAIChatCompletionParams kept for Gemini call sites.
func (m *ModelContext) BuildGeminiParams(model string) openai.ChatCompletionNewParams {
	return m.BuildOpenAIChatCompletionParams(model)
}

// OpenAIChatCompletionFunctionTool builds a Chat Completions function tool from a provider-neutral
// function spec. Reuses the Claude schema sanitizer to strip JSON-schema features that
// some providers reject, keeping shared agent tool specs portable.
func OpenAIChatCompletionFunctionTool(name, description string, properties map[string]interface{}, required []string) openai.ChatCompletionToolUnionParam {
	params := shared.FunctionParameters{
		"type":                 "object",
		"additionalProperties": false,
	}
	sanitized := sanitizeClaudeSchemaProperties(properties)
	if len(sanitized) > 0 {
		params["properties"] = sanitized
		if len(required) > 0 {
			params["required"] = required
		}
	}
	// Gemini rejects parameterless tools when parameters is an empty object; omit it.
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        name,
		Description: openai.String(description),
		Parameters:  params,
	})
}

// GeminiFunctionTool is an alias for OpenAIChatCompletionFunctionTool kept for Gemini call sites.
func GeminiFunctionTool(name, description string, properties map[string]interface{}, required []string) openai.ChatCompletionToolUnionParam {
	return OpenAIChatCompletionFunctionTool(name, description, properties, required)
}
