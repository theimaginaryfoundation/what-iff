package provider

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
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
	// This helper always requests final usage because token accounting drives
	// usage metering and telemetry. OpenAI-compatible APIs send complete usage
	// only in the final empty chunk when explicitly requested. Providers that
	// omit it still produce a valid response with zero usage.
	params.StreamOptions.IncludeUsage = openai.Bool(true)
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if len(chunk.Choices) > 0 {
			if d := chunk.Choices[0].Delta.Content; d != "" {
				if onTextDelta != nil {
					onTextDelta(d)
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
	return &acc.ChatCompletion, nil
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
