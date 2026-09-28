package provider

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// App-level retry policy for the OpenAI Responses and Claude streaming calls.
//
// These calls are the only retry layer for their requests: each HTTP request they make passes
// the SDK's WithMaxRetries(0), so one logical call is at most llmMaxAttempts HTTP attempts.
// The loop lives here rather than in the SDK because it needs to know whether the attempt
// already streamed text (a retry would duplicate it), and it is what counts
// whatiff.gen_ai.retries. Calls without this loop (Claude non-streaming, the Chat Completions
// providers, embeddings, files, images) keep the SDK's own retries instead.
const llmMaxAttempts = 3

// Wait before retry n (0-based). Rate-limit waits span a per-minute quota window; a
// Retry-After header shortens them (see retryWait).
var (
	rateLimitRetryWaits   = [llmMaxAttempts - 1]time.Duration{65 * time.Second, 100 * time.Second}
	serverErrorRetryWaits = [llmMaxAttempts - 1]time.Duration{5 * time.Second, 30 * time.Second}
)

// sdkNoRetries is passed on every request made inside retryLLMCall.
const sdkNoRetries = 0

// retryLLMCall runs attempt up to llmMaxAttempts times and ends call with the final outcome, so
// the call's duration covers every attempt and wait. attempt reports whether it already
// streamed text, which makes any failure final. Each retry is counted on call and logged to
// log. A nil call records nothing (genAICall's end and retry return early on a nil receiver)
// and a nil log logs nothing.
func retryLLMCall[T any](
	ctx context.Context,
	call *genAICall,
	log *zap.Logger,
	attempt func(context.Context) (*T, bool, error),
) (resp *T, err error) {
	if log == nil {
		log = zap.NewNop()
	}
	if call != nil {
		log = log.With(zap.String("provider", call.provider), zap.String("model", call.model))
	}
	defer func() { call.end(err) }()
	for n := 0; ; n++ {
		resp, streamedText, err := attempt(ctx)
		if err == nil {
			return resp, nil
		}
		reason, retryable := retryReason(err)
		if !retryable || streamedText || n >= llmMaxAttempts-1 {
			return nil, err
		}
		wait := retryWait(reason, n, err)
		call.retry(reason)
		log.Warn("llm call failed; retrying",
			zap.String("reason", reason),
			zap.Int("attempt", n+1),
			zap.Int("max_attempts", llmMaxAttempts),
			zap.Duration("wait", wait),
			zap.Error(err))
		if waitErr := waitForRetry(ctx, wait); waitErr != nil {
			return nil, waitErr
		}
	}
}

// retryReason maps a failed attempt to its whatiff.gen_ai.retries reason, or false when it
// shouldn't be retried. It goes by telemetry.ClassifyError, so the decision follows the SDK
// error's status code (429, any 5xx including Anthropic's 529) rather than its message, and
// cancellation and timeouts are never retried.
func retryReason(err error) (string, bool) {
	switch telemetry.ClassifyError(err) {
	case telemetry.ErrorTypeRateLimited:
		return retryReasonRateLimited, true
	case telemetry.ErrorTypeServer:
		return retryReasonServerError, true
	case telemetry.ErrorTypeNetwork:
		// No response at all (connection refused or reset). The SDK retried these before it
		// was turned off for these calls.
		return retryReasonNetwork, true
	}
	return "", false
}

// retryWait is the wait before retry n: the scheduled wait for reason, or the provider's
// Retry-After when that is shorter.
func retryWait(reason string, n int, err error) time.Duration {
	schedule := serverErrorRetryWaits
	if reason == retryReasonRateLimited {
		schedule = rateLimitRetryWaits
	}
	wait := schedule[max(0, min(n, len(schedule)-1))]
	if after, ok := retryAfter(err); ok && after < wait {
		return after
	}
	return wait
}

// retryAfter reads Retry-After-Ms or Retry-After (seconds) from an SDK error's response.
func retryAfter(err error) (time.Duration, bool) {
	var res *http.Response
	var oaiErr *openai.Error
	var antErr *anthropic.Error
	switch {
	case errors.As(err, &oaiErr) && oaiErr != nil:
		res = oaiErr.Response
	case errors.As(err, &antErr) && antErr != nil:
		res = antErr.Response
	}
	if res == nil {
		return 0, false
	}
	if ms, perr := strconv.ParseFloat(res.Header.Get("Retry-After-Ms"), 64); perr == nil && ms >= 0 {
		return time.Duration(ms * float64(time.Millisecond)), true
	}
	if s, perr := strconv.ParseFloat(res.Header.Get("Retry-After"), 64); perr == nil && s >= 0 {
		return time.Duration(s * float64(time.Second)), true
	}
	return 0, false
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

// streamStatusError is a failure the provider reported inside a stream that had already
// started with a 200. It carries the HTTP status the same failure gets as a response, so
// ClassifyError (error.type) and the retry loop treat both alike.
type streamStatusError struct {
	err    error
	status int
}

func (e *streamStatusError) Error() string       { return e.err.Error() }
func (e *streamStatusError) Unwrap() error       { return e.err }
func (e *streamStatusError) HTTPStatusCode() int { return e.status }

// withStreamStatus wraps err with status, or returns it unchanged when status is 0.
func withStreamStatus(err error, status int) error {
	if status == 0 {
		return err
	}
	return &streamStatusError{err: err, status: status}
}

// openAIStreamErrorStatus maps the code of a Responses stream "error" or "response.failed"
// event to the matching HTTP status, or 0 when the code isn't transient.
func openAIStreamErrorStatus(code string) int {
	switch code {
	case "server_error":
		return http.StatusInternalServerError
	case "rate_limit_exceeded":
		return http.StatusTooManyRequests
	}
	return 0
}

// anthropicStreamErrorPrefix starts the error the Anthropic SDK returns for a mid-stream
// "error" event, followed by the event's JSON (pinned by
// TestClaudeStreaming_RetriesOverloadedStreamErrorEvent).
const anthropicStreamErrorPrefix = "received error while streaming: "

// anthropicStatusByErrorType maps Anthropic error types to their HTTP status. Only transient
// ones are listed; the rest are left unclassified.
var anthropicStatusByErrorType = map[string]int{
	"rate_limit_error": http.StatusTooManyRequests,
	"api_error":        http.StatusInternalServerError,
	"overloaded_error": 529,
}

// classifyAnthropicStreamError gives a mid-stream Anthropic "error" event (most often
// overloaded_error) the status it would have had as a response, so it is retried like one.
func classifyAnthropicStreamError(err error) error {
	msg := err.Error()
	if !strings.HasPrefix(msg, anthropicStreamErrorPrefix) {
		return err
	}
	errType := gjson.Get(strings.TrimPrefix(msg, anthropicStreamErrorPrefix), "error.type").String()
	return withStreamStatus(err, anthropicStatusByErrorType[errType])
}
