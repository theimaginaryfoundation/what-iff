package provider

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/tidwall/gjson"
)

// streamChatCompletion drives a streaming Chat Completions request, forwarding
// text deltas to onTextDelta as they arrive and accumulating the full response
// (content, tool calls, usage) so callers can treat the result exactly like a
// non-streaming ChatCompletion. Shared by all OpenAI-compatible providers
// (Gemini, Mistral, DeepSeek, Qwen, Xiaomi).
//
// It returns the accumulated completion or an error.
//
// No mid-stream retry guard is needed here, unlike the Claude and OpenAI
// Responses paths — those wrap their calls in application-level retry loops and
// carry a "delta already emitted" flag so a retry cannot re-send text the user
// has already seen. This path has no such loop. Its only retries come from
// openai-go's WithMaxRetries, and those are decided from the response status
// line and headers before any SSE body is read (shouldRetry inspects request
// replayability, a nil response, x-should-retry, and the status code — never
// the body), after which the loop exits and hands the raw response to the
// stream decoder. ssestream has no reconnect or resume logic of its own. So
// once a chunk has been delivered, nothing can re-issue the request.
//
// Pinned by TestStreamChatCompletion_DoesNotRetryAfterDeltasDelivered, which
// aborts a stream after real content and asserts the server saw exactly one
// request, with a sibling test proving the counter can observe a retry when the
// failure precedes the body. Verified against openai-go v3.29.0; an SDK bump
// that changed this would fail those tests rather than pass quietly.
func streamChatCompletion(
	ctx context.Context,
	client *openai.Client,
	params openai.ChatCompletionNewParams,
	onTextDelta func(delta string),
) (*openai.ChatCompletion, error) {
	return streamChatCompletionCapturing(ctx, client, params, chatCompletionStreamHooks{onTextDelta: onTextDelta})
}

// chatCompletionStreamHooks are the optional per-chunk callbacks for
// streamChatCompletionCapturing; nil hooks are skipped.
type chatCompletionStreamHooks struct {
	// onTextDelta receives incremental assistant content.
	onTextDelta func(delta string)
	// onToolCallDelta receives the raw JSON of each streamed tool-call delta (and its index).
	onToolCallDelta func(index int64, raw string)
	// onReasoningDelta receives incremental reasoning_content — the non-standard field
	// DeepSeek-style reasoning models (Xiaomi MiMo) stream their reasoning in.
	onReasoningDelta func(delta string)
}

// streamChatCompletionCapturing is streamChatCompletion with the full hook set.
// ChatCompletionAccumulator keeps only standard fields, so non-standard ones —
// Gemini's extra_content.google.thought_signature, which Google requires echoed
// back on the assistant tool-call turn, and MiMo's reasoning_content — are lost
// unless captured here from the raw delta.
func streamChatCompletionCapturing(
	ctx context.Context,
	client *openai.Client,
	params openai.ChatCompletionNewParams,
	hooks chatCompletionStreamHooks,
) (*openai.ChatCompletion, error) {
	// This helper always requests final usage because token accounting drives
	// usage metering and telemetry. OpenAI-compatible APIs send complete usage
	// only in the final empty chunk when explicitly requested. Providers that
	// omit it still produce a valid response with zero usage.
	params.StreamOptions.IncludeUsage = openai.Bool(true)
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	acc := openai.ChatCompletionAccumulator{}
	// The accumulator SUMS chunk.Usage across every chunk (see
	// ChatCompletionAccumulator.accumulateDelta: `cc.Usage.X += chunk.Usage.X`).
	// That is correct for OpenAI, which reports usage only in the final chunk, but
	// wrong for providers whose OpenAI-compatible streaming repeats usage on every
	// chunk. Gemini emits a full, cumulative usage block on each SSE chunk, so the
	// summed total is inflated by roughly the chunk count (an 80k prompt reported as
	// 300k once the response is long enough). Capture the usage from the last chunk
	// that carries it and use that as the authoritative total instead of the sum.
	var finalUsage openai.CompletionUsage
	var sawUsage bool
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if chunkCarriesUsage(chunk.Usage) {
			finalUsage = chunk.Usage
			sawUsage = true
		}
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if d := delta.Content; d != "" && hooks.onTextDelta != nil {
				hooks.onTextDelta(d)
			}
			if hooks.onToolCallDelta != nil {
				for _, tc := range delta.ToolCalls {
					if raw := tc.RawJSON(); raw != "" {
						hooks.onToolCallDelta(tc.Index, raw)
					}
				}
			}
			if hooks.onReasoningDelta != nil {
				if d := gjson.Get(delta.RawJSON(), "reasoning_content").String(); d != "" {
					hooks.onReasoningDelta(d)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	if acc.ChatCompletion.ID == "" {
		return nil, fmt.Errorf("chat completion stream finished with no chunks")
	}
	if sawUsage {
		acc.ChatCompletion.Usage = finalUsage
	}
	return &acc.ChatCompletion, nil
}

// ChatCompletionReasoning returns the non-standard reasoning_content from the first
// choice of a non-streamed Chat Completions response ("" when absent). Streamed
// responses lose it in the accumulator; capture those via onReasoningDelta instead.
func ChatCompletionReasoning(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return gjson.Get(resp.Choices[0].Message.RawJSON(), "reasoning_content").String()
}

// chatCompletionFinishReason returns the first choice's finish_reason ("stop",
// "length", "tool_calls", …), or "" when there is none.
func chatCompletionFinishReason(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].FinishReason
}

// chatCompletionTruncatedWithoutText reports whether resp was cut off at the length
// limit before producing any reply text — the whole output cap went to reasoning or a
// (necessarily partial) tool call. Such a response is unusable as-is.
func chatCompletionTruncatedWithoutText(resp *openai.ChatCompletion) bool {
	return chatCompletionFinishReason(resp) == "length" && ExtractChatCompletionText(resp) == ""
}

// chunkCarriesUsage reports whether a streamed chunk's usage block was populated
// (as opposed to the zero value the SDK leaves on chunks that omit usage).
func chunkCarriesUsage(u openai.CompletionUsage) bool {
	return u.PromptTokens != 0 || u.CompletionTokens != 0 || u.TotalTokens != 0
}

// chatCompletionTokenUsage safely extracts prompt/completion token counts from a
// Chat Completions response. A response with no usage has zero-valued token fields,
// and ChatCompletionAccumulator does not preserve JSON field-presence metadata, so
// this reads the fields directly rather than relying on Usage.Valid().
func chatCompletionTokenUsage(resp *openai.ChatCompletion) (promptTokens, completionTokens int64) {
	if resp == nil {
		return 0, 0
	}
	return resp.Usage.PromptTokens, resp.Usage.CompletionTokens
}
