package provider

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

type OpenAIProvider struct {
	ds           *datastore.Datastore
	oaiClient    *openai.Client
	tokenCounter *TokenCounter
	fileStore    storage.FileStore
	tel          *telemetry.Telemetry
}

func NewOpenAIProvider(ds *datastore.Datastore, oaiClient *openai.Client, fileStore storage.FileStore, tel *telemetry.Telemetry) *OpenAIProvider {
	return &OpenAIProvider{
		ds:           ds,
		oaiClient:    oaiClient,
		tokenCounter: NewTokenCounter(),
		fileStore:    fileStore,
		tel:          tel,
	}
}

// zapLog returns the configured logger or a no-op logger when telemetry is nil (e.g. tests).
func (a *OpenAIProvider) zapLog() *zap.Logger {
	if a != nil && a.tel != nil && a.tel.Logger != nil {
		return a.tel.Logger
	}
	return zap.NewNop()
}

// responsesNew is the single entry point for Responses API HTTP calls; records token usage metrics.
func (c *OpenAIProvider) responsesNew(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	resp, err := c.oaiClient.Responses.New(ctx, params)
	if err != nil {
		return nil, err
	}
	recordProviderTokenUsage(ctx, c.tel, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	return resp, nil
}

func (c *OpenAIProvider) responsesNewStreaming(
	ctx context.Context,
	params responses.ResponseNewParams,
	onTextDelta func(delta string),
) (*responses.Response, bool, error) {
	stream := c.oaiClient.Responses.NewStreaming(ctx, params)
	defer stream.Close()

	var finalResp *responses.Response
	deltaEmitted := false

	for stream.Next() {
		ev := stream.Current()
		if ev.Type == "response.output_text.delta" {
			if onTextDelta != nil && ev.Delta != "" {
				onTextDelta(ev.Delta)
			}
			if ev.Delta != "" {
				deltaEmitted = true
			}
			continue
		}
		if ev.Type == "response.completed" {
			completed := ev.AsResponseCompleted()
			resp := completed.Response
			finalResp = &resp
			continue
		}
		if ev.Type == "response.incomplete" {
			incomplete := ev.AsResponseIncomplete()
			resp := incomplete.Response
			if resp.IncompleteDetails.Reason != "max_output_tokens" {
				continue
			}
			for _, output := range resp.Output {
				if output.Type == "function_call" {
					finalResp = &resp
					break
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, deltaEmitted, WrapCanceledWithUsage(err, CancelUsage{
				Available: false,
				Source:    "openai_stream_usage_unavailable",
			})
		}
		return nil, deltaEmitted, err
	}
	if finalResp == nil {
		return nil, deltaEmitted, fmt.Errorf("stream finished without response.completed event")
	}
	recordProviderTokenUsage(ctx, c.tel, finalResp.Usage.InputTokens, finalResp.Usage.OutputTokens)
	return finalResp, deltaEmitted, nil
}

// CallWithRetry implements retry logic for API calls using the Responses API
func (c *OpenAIProvider) CallWithRetry(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	return c.callWithRetry(ctx, func(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, bool, error) {
		resp, err := c.responsesNew(ctx, params)
		return resp, false, err
	}, params)
}

// CallWithRetryStreaming calls the streaming Responses API and forwards text deltas to onTextDelta.
// If a retryable error occurs after at least one delta was emitted, it returns immediately to avoid
// duplicate persisted chunks.
func (c *OpenAIProvider) CallWithRetryStreaming(
	ctx context.Context,
	params responses.ResponseNewParams,
	onTextDelta func(delta string),
) (*responses.Response, error) {
	return c.callWithRetry(ctx, func(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, bool, error) {
		return c.responsesNewStreaming(ctx, params, onTextDelta)
	}, params)
}

func (c *OpenAIProvider) callWithRetry(
	ctx context.Context,
	caller func(context.Context, responses.ResponseNewParams) (*responses.Response, bool, error),
	params responses.ResponseNewParams,
) (*responses.Response, error) {
	const maxRetries = 3
	rateLimitWaitTimes := []time.Duration{65 * time.Second, 100 * time.Second, 135 * time.Second}
	serverErrorWaitTimes := []time.Duration{5 * time.Second, 30 * time.Second, 60 * time.Second}

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, streamedAnyDelta, err := caller(ctx, params)
		if err != nil {
			if isRateLimitError(err) {
				waitTime := rateLimitWaitTimes[attempt]
				if attempt < maxRetries-1 {
					if streamedAnyDelta {
						return nil, err
					}
					c.zapLog().Warn("openai rate limited; retrying",
						zap.Int("attempt", attempt+1),
						zap.Int("max_retries", maxRetries),
						zap.Duration("wait", waitTime),
						zap.Error(err))
					if waitErr := waitForRetry(ctx, waitTime); waitErr != nil {
						return nil, waitErr
					}
					continue
				}
			} else if isServerError(err) {
				waitTime := serverErrorWaitTimes[attempt]
				if attempt < maxRetries-1 {
					if streamedAnyDelta {
						return nil, err
					}
					c.zapLog().Warn("openai server error; retrying",
						zap.Int("attempt", attempt+1),
						zap.Int("max_retries", maxRetries),
						zap.Duration("wait", waitTime),
						zap.Error(err))
					if waitErr := waitForRetry(ctx, waitTime); waitErr != nil {
						return nil, waitErr
					}
					continue
				}
			}
			return nil, err
		}
		return resp, nil
	}

	return nil, fmt.Errorf("failed after %d attempts due to OpenAI API issues", maxRetries)
}

func waitForRetry(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *OpenAIProvider) CountTokens(text string) (int, error) {
	return c.tokenCounter.CountTokens(text)
}

func (c *OpenAIProvider) SelectCarryOverTurns(recent []*models.ChatMessage, maxTurns, maxTokens int) [][2]*models.ChatMessage {
	return c.tokenCounter.SelectCarryOverTurns(recent, maxTurns, maxTokens)
}

// processResponseOutput extracts and deduplicates content from OpenAI response output
// This addresses issues where parallel tool calling can result in duplicate content
func ProcessResponseOutput(resp *responses.Response) string {
	if resp == nil {
		return ""
	}

	// First, try to get the raw output text
	rawOutput := resp.OutputText()

	// If the output is empty or very short, return as-is (no duplication possible)
	if len(strings.TrimSpace(rawOutput)) < shortMessageThreshold {
		return rawOutput
	}

	// Split the output into paragraphs and deduplicate
	paragraphs := strings.Split(rawOutput, "\n\n")

	return StripOpenAIFileLinks(strings.Join(dedupeParagraphs(paragraphs), "\n\n"))
}

func dedupeParagraphs(paragraphs []string) []string {
	var uniqueParagraphs []string
	seenContent := make(map[string]bool)

	for _, paragraph := range paragraphs {
		trimmedParagraph := strings.TrimSpace(paragraph)

		// Preserve empty paragraphs for proper markdown formatting
		// (they're needed for spacing between blocks, tables, etc.)
		if trimmedParagraph == "" {
			uniqueParagraphs = append(uniqueParagraphs, paragraph)
			continue
		}

		// Create a hash of the paragraph content for deduplication
		// We normalize whitespace to catch minor formatting differences
		normalizedContent := strings.Join(strings.Fields(trimmedParagraph), " ")
		contentHash := fmt.Sprintf("%x", md5.Sum([]byte(normalizedContent)))

		// Only add if we haven't seen this content before
		if !seenContent[contentHash] {
			uniqueParagraphs = append(uniqueParagraphs, trimmedParagraph)
			seenContent[contentHash] = true
		}
	}
	return uniqueParagraphs
}

// isRateLimitError checks if an error is a rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "too many requests")
}

// isServerError checks if an error is a server error
func isServerError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "internal server error") ||
		strings.Contains(errStr, "server_error")
}
