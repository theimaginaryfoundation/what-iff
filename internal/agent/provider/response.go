package provider

import (
	"strings"
	"time"

	"github.com/openai/openai-go/v3/responses"
)

// GenerateResponse is a provider-agnostic result from a single chat generation call.
// Both the OpenAI and Claude providers normalise their raw responses into this type
// so that the agent core does not need to import provider-specific response types.
type GenerateResponse struct {
	ID           string
	InputTokens  int64
	OutputTokens int64
	// CreatedAt is a Unix timestamp. OpenAI supplies this from the response; Claude
	// providers set it to time.Now() since the Messages API does not return a timestamp.
	CreatedAt int64
	Text      string
	// StopReason is the provider's own account of why generation ended, verbatim and
	// un-normalised ("end_turn", "max_tokens", "max_output_tokens", "refusal",
	// "incomplete", …). Without it a turn that ends without usable text is
	// indistinguishable from one that ends normally, which is exactly the case that used
	// to be persisted as a blank assistant message. The empty-turn guard
	// (assertGenerationProducedOutput) reads it to tell an output-length truncation apart
	// from other empty causes and surface a clearer message (see isTruncationStopReason);
	// otherwise nothing branches on it. Empty when the provider reports nothing.
	StopReason string
	// Reasoning is the model's reasoning/thinking text for the whole turn — every round
	// of the tool loop, not just this final call — as reported by providers that expose
	// it (z.ai GLM thinking blocks, Xiaomi MiMo reasoning_content). Persisted for display
	// only; it is never replayed to the model. Empty when the provider reported none.
	Reasoning string
}

// reasoningLog accumulates per-call reasoning text across a turn's tool rounds so the
// final GenerateResponse can carry all of it. Adapters embed it and call add after
// every provider response.
type reasoningLog struct {
	parts []string
}

func (r *reasoningLog) add(text string) {
	if t := strings.TrimSpace(text); t != "" {
		r.parts = append(r.parts, t)
	}
}

// String joins the rounds with a blank line between them.
func (r *reasoningLog) String() string {
	return strings.Join(r.parts, "\n\n")
}

// OpenAIToGenerateResponse converts an OpenAI Responses API response into a GenerateResponse.
func OpenAIToGenerateResponse(resp *responses.Response) *GenerateResponse {
	if resp == nil {
		return &GenerateResponse{}
	}
	return &GenerateResponse{
		ID:           resp.ID,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		CreatedAt:    int64(resp.CreatedAt),
		Text:         ProcessResponseOutput(resp),
		StopReason:   openAIStopReason(resp),
	}
}

// openAIStopReason reports why a Responses call ended. The Responses API splits this
// across two fields: a coarse status, plus a reason that is only populated when the
// status is "incomplete". Prefer the specific one.
func openAIStopReason(resp *responses.Response) string {
	if reason := strings.TrimSpace(resp.IncompleteDetails.Reason); reason != "" {
		return reason
	}
	return string(resp.Status)
}

// ClaudeGenerateResponse builds a GenerateResponse from raw Anthropic response values.
func ClaudeGenerateResponse(id string, inputTokens, outputTokens int64, text string) *GenerateResponse {
	return &GenerateResponse{
		ID:           id,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CreatedAt:    time.Now().Unix(),
		Text:         text,
	}
}

// reasoningRelay forwards live reasoning deltas for one adapter's turn to a
// ReasoningStream, keeping the live draft consistent with the reasoningLog that
// becomes GenerateResponse.Reasoning: it inserts the blank-line separator between
// calls, and on reset (a streamed attempt was discarded — a transport retry or the
// truncation fallback) it clears the consumer and re-sends the kept rounds.
type reasoningRelay struct {
	stream ReasoningStream
	// kept is the adapter's log of reasoning from calls that were kept this turn.
	kept *reasoningLog
	// emittedThisCall reports whether the current call has streamed any reasoning yet.
	emittedThisCall bool
}

// enabled reports whether anyone is listening.
func (r *reasoningRelay) enabled() bool {
	return r != nil && r.stream.OnDelta != nil
}

// beginCall marks the start of a new provider call.
func (r *reasoningRelay) beginCall() {
	if r != nil {
		r.emittedThisCall = false
	}
}

// delta forwards one reasoning chunk, prefixing the round separator on the first
// chunk of a call that follows kept reasoning.
func (r *reasoningRelay) delta(d string) {
	if !r.enabled() || d == "" {
		return
	}
	if !r.emittedThisCall && r.kept != nil && len(r.kept.parts) > 0 {
		d = "\n\n" + d
	}
	r.emittedThisCall = true
	r.stream.OnDelta(d)
}

// reset discards the current call's streamed reasoning. No-op when nothing was
// streamed for it, so callers can invoke it unconditionally before a retry.
func (r *reasoningRelay) reset() {
	if !r.enabled() || !r.emittedThisCall {
		return
	}
	r.emittedThisCall = false
	if r.stream.OnReset != nil {
		r.stream.OnReset()
	}
	if r.kept != nil {
		if kept := r.kept.String(); kept != "" {
			r.stream.OnDelta(kept)
		}
	}
}
