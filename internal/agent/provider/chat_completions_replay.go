package provider

import (
	"strings"

	"github.com/openai/openai-go/v3"
)

// chatCompletionAssistantReplay converts an assistant tool-call response into the
// request message that is appended to the conversation for the next tool round.
//
// It exists because the SDK's Message.ToParam() is unsafe for streamed turns: tool
// calls assembled by ChatCompletionAccumulator carry no raw JSON, ToParam() installs
// that empty string as the marshal override, and the next request fails with
// "unexpected end of JSON input". Each tool call is rebuilt from its decoded fields
// instead. (Gemini has its own variant, geminiAssistantToolCallMessage, which also
// re-attaches Google's thought signature.)
//
// A non-empty reasoningContent is echoed back as the non-standard reasoning_content
// field. MiMo requires it on every assistant tool-call message while thinking is
// enabled (else a 400), and the stream accumulator drops it, so it has to come from
// the reasoning the adapter captured for that call.
func chatCompletionAssistantReplay(msg openai.ChatCompletionMessage, reasoningContent string) openai.ChatCompletionMessageParamUnion {
	p := msg.ToAssistantMessageParam()
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
