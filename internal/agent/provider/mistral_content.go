package provider

import (
	"context"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/tidwall/gjson"
)

// Mistral's hybrid models (ADR 0x026) answer with content as a plain string, or, while reasoning,
// as an array of typed chunks: {"type":"thinking","thinking":[{"type":"text","text":...}]} then
// {"type":"text","text":...}. Streamed deltas switch between the two shapes mid-stream. openai-go
// types content as a string and would hand back the array as raw JSON text, so Mistral content is
// read from the raw JSON here.

// mistralContent splits a content value into reply text and thinking.
func mistralContent(content gjson.Result) (text, thinking string) {
	if !content.IsArray() {
		return content.String(), ""
	}
	var t, th strings.Builder
	for _, chunk := range content.Array() {
		switch chunk.Get("type").String() {
		case "thinking":
			for _, part := range chunk.Get("thinking").Array() {
				th.WriteString(part.Get("text").String())
			}
		case "text":
			t.WriteString(chunk.Get("text").String())
		}
	}
	return t.String(), th.String()
}

// mistralDeltaContent is a chatCompletionStreamHooks.parseContent for Mistral deltas.
func mistralDeltaContent(rawDelta string) (text, reasoning string) {
	return mistralContent(gjson.Get(rawDelta, "content"))
}

// mistralSender is a reasoningChatSender that reads Mistral's content chunks.
func mistralSender(tel *telemetry.Telemetry, client *openai.Client) reasoningChatSender {
	return func(ctx context.Context, params openai.ChatCompletionNewParams, onText, onReasoning func(string)) (*openai.ChatCompletion, string, error) {
		if onText == nil {
			resp, err := chatCompletionsNew(ctx, tel, telemetry.DependencyMistral, client, params)
			if err != nil {
				return nil, "", err
			}
			if len(resp.Choices) == 0 {
				return resp, "", nil
			}
			text, thinking := mistralContent(gjson.Get(resp.Choices[0].Message.RawJSON(), "content"))
			resp.Choices[0].Message.Content = text
			return resp, thinking, nil
		}
		var thinking strings.Builder
		resp, err := chatCompletionsStream(ctx, tel, telemetry.DependencyMistral, client, params, chatCompletionStreamHooks{
			onTextDelta:  onText,
			parseContent: mistralDeltaContent,
			onReasoningDelta: func(d string) {
				thinking.WriteString(d)
				if onReasoning != nil {
					onReasoning(d)
				}
			},
		})
		if err != nil {
			return nil, "", err
		}
		return resp, thinking.String(), nil
	}
}

// mistralChunkReplay sends a turn's thinking back as Mistral's own thinking chunk at the head of
// the assistant content, as its docs ask ("do not strip ThinkChunk from assistant messages before
// replaying them"). A turn without thinking is replayed as usual.
type mistralChunkReplay struct{}

func (mistralChunkReplay) toolTurn(msg openai.ChatCompletionMessage, reasoning string) openai.ChatCompletionMessageParamUnion {
	out := chatCompletionAssistantReplay(msg, "")
	if strings.TrimSpace(reasoning) != "" {
		out.OfAssistant.SetExtraFields(map[string]any{"content": mistralThinkingContent(msg.Content, reasoning)})
	}
	return out
}

func (mistralChunkReplay) finalTurn(text, reasoning string) openai.ChatCompletionMessageParamUnion {
	p := openai.ChatCompletionAssistantMessageParam{
		Content: openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(text)},
	}
	if strings.TrimSpace(reasoning) != "" {
		p.SetExtraFields(map[string]any{"content": mistralThinkingContent(text, reasoning)})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &p}
}

// mistralThinkingContent is an assistant content array: the thinking chunk, then the text.
func mistralThinkingContent(text, thinking string) []any {
	return []any{
		map[string]any{"type": "thinking", "thinking": []any{map[string]any{"type": "text", "text": thinking}}},
		map[string]any{"type": "text", "text": text},
	}
}
