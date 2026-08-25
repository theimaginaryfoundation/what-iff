package provider

import (
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
	}
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
