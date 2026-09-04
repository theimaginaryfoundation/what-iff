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
	// un-normalised ("end_turn", "max_tokens", "refusal", "incomplete", …). It is
	// diagnostic only — nothing branches on it — but without it a turn that ends without
	// usable text is indistinguishable from one that ends normally, which is exactly the
	// case that used to be persisted as a blank assistant message. Empty when the
	// provider reports nothing.
	StopReason string
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
