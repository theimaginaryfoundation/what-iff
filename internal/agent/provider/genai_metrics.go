package provider

import (
	"context"
	"errors"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/tidwall/gjson"
	"go.opentelemetry.io/otel/attribute"
)

// gen_ai.operation.name values for GenAIOperationDuration (embeddings are recorded by the
// embedding package).
const (
	genAIOpChat          = "chat"
	genAIOpGenerateImage = "generate_image"
	genAIOpEditImage     = "edit_image"
	genAIOpFile          = "file"
)

// genAIModelNone labels calls that have no model (the Files API).
const genAIModelNone = "none"

// Retry reasons for GenAIRetries.
const (
	retryReasonRateLimited = "rate_limited"
	retryReasonServerError = "server_error"
	retryReasonTruncated   = "truncated"
	retryReasonLength      = "length"
)

// init teaches telemetry.ClassifyError the SDK error types, so error.type is status based
// (rate_limited, server_error, auth...) everywhere those errors surface, not just here.
func init() {
	telemetry.RegisterStatusCodeFunc(func(err error) (int, bool) {
		var apiErr *openai.Error
		if errors.As(err, &apiErr) && apiErr != nil {
			return apiErr.StatusCode, true
		}
		return 0, false
	})
	telemetry.RegisterStatusCodeFunc(func(err error) (int, bool) {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && apiErr != nil {
			return apiErr.StatusCode, true
		}
		return 0, false
	})
}

// genAIUsage is the provider-reported token usage of one call. CachedInput is a subset of
// Input and Reasoning a subset of Output.
type genAIUsage struct {
	Input       int64
	Output      int64
	CachedInput int64
	Reasoning   int64
}

// genAICall records the metrics of one logical LLM call: its duration (all app-level retries
// included), time to first token for streams, token usage, retries and safety blocks. Every
// method is safe on a nil *genAICall, so tests can pass nil to the wrappers.
type genAICall struct {
	ctx       context.Context
	metrics   *telemetry.Metrics
	provider  string
	model     string
	operation string
	callPath  string

	start        time.Time
	attemptStart time.Time
	ttft         time.Duration
	sawToken     bool
	safetyBlock  bool
	ended        bool
}

// startGenAICall starts timing a call. provider is a telemetry.Dependency* name and model the
// requested model id (bounded by the model catalog).
func startGenAICall(ctx context.Context, tel *telemetry.Telemetry, provider, model, operation string) *genAICall {
	var m *telemetry.Metrics
	if tel != nil {
		m = tel.Metrics
	}
	if model == "" {
		model = "unknown"
	}
	now := time.Now()
	return &genAICall{
		ctx:          ctx,
		metrics:      m,
		provider:     provider,
		model:        model,
		operation:    operation,
		callPath:     string(telemetry.CallPathFromContext(ctx)),
		start:        now,
		attemptStart: now,
	}
}

// beginAttempt marks the start of one attempt inside an app-level retry loop, so time to first
// token is measured from the attempt that succeeded rather than from the first attempt.
func (c *genAICall) beginAttempt() {
	if c == nil {
		return
	}
	c.attemptStart = time.Now()
	c.sawToken = false
	c.ttft = 0
}

// firstToken notes the first streamed output (text or reasoning) of the current attempt.
func (c *genAICall) firstToken() {
	if c == nil || c.sawToken {
		return
	}
	c.sawToken = true
	c.ttft = time.Since(c.attemptStart)
}

// retry counts an app-level retry or fallback of this call.
func (c *genAICall) retry(reason string) {
	if c == nil {
		return
	}
	c.metrics.Add(c.ctx, telemetry.GenAIRetries, 1,
		telemetry.AttrGenAIProvider.String(c.provider),
		telemetry.AttrGenAIModel.String(c.model),
		telemetry.AttrReason.String(reason))
}

// blocked marks a response that the provider's safety system refused without an error (a
// content_filter / refusal stop reason). end counts it once.
func (c *genAICall) blocked() {
	if c != nil {
		c.safetyBlock = true
	}
}

// recordUsage records token usage: the per-call distribution without a model label, and
// per-model totals on the counter. Only types with a positive count are recorded.
func (c *genAICall) recordUsage(u genAIUsage) {
	if c == nil || c.metrics == nil {
		return
	}
	provider := telemetry.AttrGenAIProvider.String(c.provider)
	model := telemetry.AttrGenAIModel.String(c.model)
	callPath := telemetry.AttrCallPath.String(c.callPath)
	for _, t := range []struct {
		name string
		n    int64
	}{
		{telemetry.TokenTypeInput, u.Input},
		{telemetry.TokenTypeOutput, u.Output},
		{telemetry.TokenTypeCachedInput, u.CachedInput},
		{telemetry.TokenTypeReasoning, u.Reasoning},
	} {
		if t.n <= 0 {
			continue
		}
		tokenType := telemetry.AttrGenAITokenType.String(t.name)
		c.metrics.Record(c.ctx, telemetry.GenAITokenUsage, float64(t.n), provider, tokenType, callPath)
		c.metrics.Add(c.ctx, telemetry.GenAITokens, t.n, provider, model, tokenType, callPath)
	}
}

// end records the call's duration (with error.type on failure), its time to first token when
// it succeeded after streaming, and a safety block when the error or response was one. Calls
// after the first are ignored.
func (c *genAICall) end(err error) {
	if c == nil || c.ended {
		return
	}
	c.ended = true
	if c.metrics == nil {
		return
	}
	provider := telemetry.AttrGenAIProvider.String(c.provider)
	model := telemetry.AttrGenAIModel.String(c.model)
	callPath := telemetry.AttrCallPath.String(c.callPath)
	attrs := append([]attribute.KeyValue{provider, model, telemetry.AttrGenAIOperation.String(c.operation), callPath},
		telemetry.ErrorAttrs(err)...)
	c.metrics.RecordDuration(c.ctx, telemetry.GenAIOperationDuration, time.Since(c.start), attrs...)
	if err == nil && c.sawToken {
		c.metrics.RecordDuration(c.ctx, telemetry.GenAITimeToFirstToken, c.ttft, provider, model, callPath)
	}
	if err != nil {
		if _, ok := IsSafetyViolationError(err); ok {
			c.safetyBlock = true
		}
	}
	if c.safetyBlock {
		c.metrics.Add(c.ctx, telemetry.GenAISafetyBlocks, 1, provider, model, callPath)
	}
}

// responsesUsage extracts token usage from an OpenAI Responses API response.
func responsesUsage(u responses.ResponseUsage) genAIUsage {
	return genAIUsage{
		Input:       u.InputTokens,
		Output:      u.OutputTokens,
		CachedInput: u.InputTokensDetails.CachedTokens,
		Reasoning:   u.OutputTokensDetails.ReasoningTokens,
	}
}

// anthropicUsage extracts token usage from an Anthropic Messages response. Input keeps the
// existing total (uncached + cache reads + cache writes, see claudeTotalInputTokens).
// CachedInput counts cache reads only: cache writes are billed above the base input rate and
// are not hits, so counting them would overstate the cache hit rate. Anthropic doesn't report
// thinking tokens separately, so Reasoning stays zero.
func anthropicUsage(u anthropic.Usage) genAIUsage {
	return genAIUsage{
		Input:       anthropicUsageInputTokens(u),
		Output:      anthropicUsageOutputTokens(u),
		CachedInput: u.CacheReadInputTokens,
	}
}

// anthropicBetaUsage is anthropicUsage for Beta Messages responses.
func anthropicBetaUsage(u anthropic.BetaUsage) genAIUsage {
	return genAIUsage{
		Input:       anthropicBetaUsageInputTokens(u),
		Output:      anthropicBetaUsageOutputTokens(u),
		CachedInput: u.CacheReadInputTokens,
	}
}

// chatCompletionUsage extracts token usage from a Chat Completions response. DeepSeek reports
// cache hits as prompt_cache_hit_tokens instead of prompt_tokens_details.cached_tokens.
func chatCompletionUsage(resp *openai.ChatCompletion) genAIUsage {
	if resp == nil {
		return genAIUsage{}
	}
	input, output := chatCompletionTokenUsage(resp)
	cached := resp.Usage.PromptTokensDetails.CachedTokens
	if cached == 0 {
		if raw := resp.Usage.RawJSON(); raw != "" {
			cached = gjson.Get(raw, "prompt_cache_hit_tokens").Int()
		}
	}
	return genAIUsage{
		Input:       input,
		Output:      output,
		CachedInput: cached,
		Reasoning:   resp.Usage.CompletionTokensDetails.ReasoningTokens,
	}
}

// endChatCompletion finishes a Chat Completions call: usage, content-filter blocks, duration.
func (c *genAICall) endChatCompletion(resp *openai.ChatCompletion, err error) {
	if err == nil && resp != nil {
		c.recordUsage(chatCompletionUsage(resp))
		if chatCompletionFinishReason(resp) == "content_filter" {
			c.blocked()
		}
	}
	c.end(err)
}
