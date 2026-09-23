package provider

import (
	"strings"

	"github.com/openai/openai-go/v3"
)

// chatCompletionAssistantReplay converts an assistant tool-call response into the
// request message that is appended to the conversation for the next tool round.
//
// Each tool call is rebuilt from its decoded fields rather than the SDK's
// Message.ToParam(), whose raw-JSON marshal override is empty for tool calls the
// stream accumulator assembled — the failure behind the Gemini "unexpected end of
// JSON input" fix. (Gemini has its own variant, geminiAssistantToolCallMessage,
// which also re-attaches Google's thought signature.)
//
// A tool-call turn with no text is sent with an explicit empty content string
// rather than no content field (see below).
//
// A non-empty reasoningContent is echoed back as the non-standard reasoning_content
// field. MiMo requires it on every assistant tool-call message while thinking is
// enabled (else a 400), and the stream accumulator drops it, so it has to come from
// the reasoning the adapter captured for that call.
func chatCompletionAssistantReplay(msg openai.ChatCompletionMessage, reasoningContent string) openai.ChatCompletionMessageParamUnion {
	p := msg.ToAssistantMessageParam()
	if len(msg.ToolCalls) > 0 && assistantContentEmpty(p.Content) {
		// A tool-call turn usually has no text, and ToAssistantMessageParam then omits
		// content altogether. Send an explicit "" instead — the shape MiMo's own
		// multi-turn tool examples send back. Unlike Gemini (which rejects "" and gets
		// geminiToolCallContentPlaceholder), nothing is shown to the model that it
		// could echo as reply text.
		p.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String("")}
	}
	if len(msg.ToolCalls) > 0 {
		p.ToolCalls = make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			p.ToolCalls = append(p.ToolCalls, chatCompletionToolCallParam(tc))
		}
	}
	if strings.TrimSpace(reasoningContent) != "" {
		// p is freshly built by ToAssistantMessageParam, which sets no extra fields,
		// so there is nothing here to merge with.
		p.SetExtraFields(map[string]any{"reasoning_content": reasoningContent})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &p}
}

// chatCompletionToolCallParam rebuilds a request tool-call param from a response tool
// call's decoded fields, giving the union a concrete variant so it never marshals
// from an empty raw-JSON override. IDs and arguments are passed through unchanged.
func chatCompletionToolCallParam(tc openai.ChatCompletionMessageToolCallUnion) openai.ChatCompletionMessageToolCallUnionParam {
	if tc.Type == "custom" {
		return openai.ChatCompletionMessageToolCallUnionParam{
			OfCustom: &openai.ChatCompletionMessageCustomToolCallParam{
				ID: tc.ID,
				Custom: openai.ChatCompletionMessageCustomToolCallCustomParam{
					Name:  tc.Custom.Name,
					Input: tc.Custom.Input,
				},
			},
		}
	}
	return openai.ChatCompletionMessageToolCallUnionParam{
		OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
			ID: tc.ID,
			Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		},
	}
}
