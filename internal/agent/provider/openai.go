package provider

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
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

// responsesNew is the single entry point for Responses API HTTP calls; records token usage
// (and content-filter blocks) on call, which callWithRetry owns and ends.
func (c *OpenAIProvider) responsesNew(ctx context.Context, params responses.ResponseNewParams, call *genAICall) (*responses.Response, error) {
	resp, err := c.oaiClient.Responses.New(ctx, params, option.WithMaxRetries(sdkNoRetries))
	if err != nil {
		return nil, err
	}
	recordResponsesOutcome(call, resp)
	return resp, nil
}

// recordResponsesOutcome records a successful Responses API response's token usage, and a
// safety block when the response was cut off by the content filter.
func recordResponsesOutcome(call *genAICall, resp *responses.Response) {
	call.recordUsage(responsesUsage(resp.Usage))
	if resp.IncompleteDetails.Reason == "content_filter" {
		call.blocked()
	}
}

// responsesNewStreaming streams one Responses API attempt. call (may be nil) gets the
// attempt's time to first token and, on success, its token usage.
func (c *OpenAIProvider) responsesNewStreaming(
	ctx context.Context,
	params responses.ResponseNewParams,
	onTextDelta func(delta string),
	call *genAICall,
) (*responses.Response, bool, error) {
	call.beginAttempt()
	stream := c.oaiClient.Responses.NewStreaming(ctx, params, option.WithMaxRetries(sdkNoRetries))
	defer stream.Close()

	var finalResp *responses.Response
	deltaEmitted := false

	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "response.output_text.delta":
			if onTextDelta != nil && ev.Delta != "" {
				onTextDelta(ev.Delta)
			}
			if ev.Delta != "" {
				deltaEmitted = true
				call.firstToken()
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if ev.Delta != "" {
				call.firstToken()
			}
		case "response.completed":
			resp := ev.AsResponseCompleted().Response
			finalResp = &resp
		case "response.incomplete":
			// A truncated-but-valid terminal response (e.g. max_output_tokens
			// spent on reasoning by a model like Sol, or a content filter).
			// This is not a failure: the non-streaming path already treats it as
			// a real response and surfaces IncompleteDetails.Reason through
			// GenerateResponse.StopReason. Failing the whole turn here was the
			// root cause of the opaque "stream finished without response.completed
			// event" errors (issue #132).
			resp := ev.AsResponseIncomplete().Response
			finalResp = &resp
			c.zapLog().Warn("openai responses stream returned incomplete response",
				zap.String("response_id", resp.ID),
				zap.String("status", string(resp.Status)),
				zap.String("incomplete_reason", resp.IncompleteDetails.Reason),
				zap.Bool("delta_emitted", deltaEmitted))
		case "response.failed":
			resp := ev.AsResponseFailed().Response
			return nil, deltaEmitted, withStreamStatus(fmt.Errorf(
				"openai responses stream failed: %s (code %q, response_id %q)",
				strings.TrimSpace(resp.Error.Message), string(resp.Error.Code), resp.ID),
				openAIStreamErrorStatus(string(resp.Error.Code)))
		case "error":
			errEv := ev.AsError()
			return nil, deltaEmitted, withStreamStatus(fmt.Errorf(
				"openai responses stream error: %s (code %q, param %q)",
				strings.TrimSpace(errEv.Message), errEv.Code, errEv.Param),
				openAIStreamErrorStatus(errEv.Code))
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
		// The stream ended cleanly (stream.Err() is nil) but never delivered a
		// terminal response event — no completed/incomplete/failed and no error.
		// That points at a truncated or dropped stream (proxy/LB timeout,
		// mid-stream disconnect) rather than a provider-signalled outcome.
		return nil, deltaEmitted, fmt.Errorf(
			"openai responses stream ended without a terminal event (no response.completed/incomplete/failed/error); likely a truncated or dropped stream (delta_emitted=%t)",
			deltaEmitted)
	}
	recordResponsesOutcome(call, finalResp)
	return finalResp, deltaEmitted, nil
}

// CallWithRetry implements retry logic for API calls using the Responses API
func (c *OpenAIProvider) CallWithRetry(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	return c.callWithRetry(ctx, func(ctx context.Context, params responses.ResponseNewParams, call *genAICall) (*responses.Response, bool, error) {
		resp, err := c.responsesNew(ctx, params, call)
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
	return c.callWithRetry(ctx, func(ctx context.Context, params responses.ResponseNewParams, call *genAICall) (*responses.Response, bool, error) {
		return c.responsesNewStreaming(ctx, params, onTextDelta, call)
	}, params)
}

// callWithRetry runs caller under retryLLMCall. It is also where the logical call's
// GenAIOperationDuration is recorded (all attempts and waits included) and retries counted.
func (c *OpenAIProvider) callWithRetry(
	ctx context.Context,
	caller func(context.Context, responses.ResponseNewParams, *genAICall) (*responses.Response, bool, error),
	params responses.ResponseNewParams,
) (*responses.Response, error) {
	call := startGenAICall(ctx, c.tel, telemetry.DependencyOpenAI, string(params.Model), genAIOpChat)
	return retryLLMCall(ctx, call, c.zapLog(), func(ctx context.Context) (*responses.Response, bool, error) {
		return caller(ctx, params, call)
	})
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
