package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/tidwall/gjson"
)

// ClaudeProvider wraps the Anthropic Messages API and exposes the same utility
// methods (CountTokens, SelectCarryOverTurns) used by the rest of the agent.
type ClaudeProvider struct {
	client       *anthropic.Client
	tokenCounter *TokenCounter
	tel          *telemetry.Telemetry
}

// DefaultZAIBaseURL is z.ai's Anthropic-compatible Messages API base URL, used for
// GLM models. z.ai mirrors the Anthropic API shape so the Claude path is reused
// wholesale; only the base URL + key differ.
const DefaultZAIBaseURL = "https://api.z.ai/api/anthropic"

// NewClaudeProvider creates a ClaudeProvider backed by the given Anthropic API key.
// httpClient overrides the SDK's default HTTP client when non-nil; mock mode
// passes DenyNetworkHTTPClient() so no provider call can leave the process.
func NewClaudeProvider(apiKey string, tel *telemetry.Telemetry, httpClient *http.Client) *ClaudeProvider {
	return NewClaudeProviderWithBaseURL(apiKey, "", tel, httpClient)
}

// NewClaudeProviderWithBaseURL creates a ClaudeProvider that speaks the Anthropic
// Messages API but optionally points at a different base URL. This is how z.ai
// GLM models are served: z.ai exposes an Anthropic-compatible endpoint, so the
// entire Claude rendering/adapter path is reused with only the base URL + key
// swapped. Pass baseURL="" for the real Anthropic API.
func NewClaudeProviderWithBaseURL(apiKey, baseURL string, tel *telemetry.Telemetry, httpClient *http.Client) *ClaudeProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		// 2 retries = 3 total attempts with SDK-managed exponential back-off
		// for 429/500/529 responses.
		option.WithMaxRetries(2),
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := anthropic.NewClient(opts...)
	return &ClaudeProvider{
		client:       &client,
		tokenCounter: NewTokenCounter(),
		tel:          tel,
	}
}

// claudeTotalInputTokens returns the full prompt-side token count for an Anthropic
// response. Anthropic reports Usage.InputTokens as the *uncached* portion only; the
// cached prefix (prompt caching) is split out into CacheReadInputTokens and
// CacheCreationInputTokens. Summing all three yields the true input size, matching
// what OpenAI already reports in a single field. Without this, chats that hit the
// cache (the common case, since we cache the system/checkpoint/scratchpad/history
// prefix) under-report input tokens by the entire cached prefix.
func claudeTotalInputTokens(u anthropic.Usage) int64 {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// claudeBetaTotalInputTokens is the beta-Messages equivalent of claudeTotalInputTokens.
func claudeBetaTotalInputTokens(u anthropic.BetaUsage) int64 {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// anthropicUsageInputTokens returns the prompt-side token count from an Anthropic
// Usage block. z.ai's Anthropic-compatible GLM endpoint sometimes reports OpenAI-style
// field names (prompt_tokens) that the SDK does not map into InputTokens.
func anthropicUsageInputTokens(u anthropic.Usage) int64 {
	if total := claudeTotalInputTokens(u); total > 0 {
		return total
	}
	raw := u.RawJSON()
	if raw == "" {
		return 0
	}
	if v := gjson.Get(raw, "prompt_tokens").Int(); v > 0 {
		return v
	}
	return gjson.Get(raw, "input_tokens").Int() +
		gjson.Get(raw, "cache_read_input_tokens").Int() +
		gjson.Get(raw, "cache_creation_input_tokens").Int()
}

// anthropicUsageOutputTokens returns completion tokens, with a RawJSON fallback for
// OpenAI-style completion_tokens on compatible shims such as z.ai GLM.
func anthropicUsageOutputTokens(u anthropic.Usage) int64 {
	if u.OutputTokens > 0 {
		return u.OutputTokens
	}
	raw := u.RawJSON()
	if raw == "" {
		return 0
	}
	if v := gjson.Get(raw, "output_tokens").Int(); v > 0 {
		return v
	}
	return gjson.Get(raw, "completion_tokens").Int()
}

func anthropicBetaUsageInputTokens(u anthropic.BetaUsage) int64 {
	if total := claudeBetaTotalInputTokens(u); total > 0 {
		return total
	}
	raw := u.RawJSON()
	if raw == "" {
		return 0
	}
	if v := gjson.Get(raw, "prompt_tokens").Int(); v > 0 {
		return v
	}
	return gjson.Get(raw, "input_tokens").Int() +
		gjson.Get(raw, "cache_read_input_tokens").Int() +
		gjson.Get(raw, "cache_creation_input_tokens").Int()
}

func anthropicBetaUsageOutputTokens(u anthropic.BetaUsage) int64 {
	if u.OutputTokens > 0 {
		return u.OutputTokens
	}
	raw := u.RawJSON()
	if raw == "" {
		return 0
	}
	if v := gjson.Get(raw, "output_tokens").Int(); v > 0 {
		return v
	}
	return gjson.Get(raw, "completion_tokens").Int()
}

// anthropicStreamUsage preserves usage emitted on individual stream events.
// Anthropic's SDK accumulates only message_delta.output_tokens; compatible APIs
// such as z.ai can instead send prompt_tokens/completion_tokens in the event
// usage object, which would otherwise be lost before the final Message exists.
type anthropicStreamUsage struct {
	inputTokens  int64
	outputTokens int64
}

func (u *anthropicStreamUsage) observe(rawEvent string) {
	usage := gjson.Get(rawEvent, "usage")
	if !usage.Exists() {
		usage = gjson.Get(rawEvent, "message.usage")
	}
	if !usage.Exists() {
		return
	}

	if input := anthropicRawUsageInputTokens(usage); input > 0 {
		u.inputTokens = input
	}
	if output := anthropicRawUsageOutputTokens(usage); output > 0 {
		u.outputTokens = output
	}
}

func anthropicRawUsageInputTokens(usage gjson.Result) int64 {
	if prompt := usage.Get("prompt_tokens").Int(); prompt > 0 {
		return prompt
	}
	return usage.Get("input_tokens").Int() +
		usage.Get("cache_read_input_tokens").Int() +
		usage.Get("cache_creation_input_tokens").Int()
}

func anthropicRawUsageOutputTokens(usage gjson.Result) int64 {
	if output := usage.Get("output_tokens").Int(); output > 0 {
		return output
	}
	return usage.Get("completion_tokens").Int()
}

func (u anthropicStreamUsage) apply(usage *anthropic.Usage) {
	if usage == nil {
		return
	}
	if claudeTotalInputTokens(*usage) == 0 && u.inputTokens > 0 {
		usage.InputTokens = u.inputTokens
	}
	if usage.OutputTokens == 0 && u.outputTokens > 0 {
		usage.OutputTokens = u.outputTokens
	}
}

func (u anthropicStreamUsage) applyBeta(usage *anthropic.BetaUsage) {
	if usage == nil {
		return
	}
	if claudeBetaTotalInputTokens(*usage) == 0 && u.inputTokens > 0 {
		usage.InputTokens = u.inputTokens
	}
	if usage.OutputTokens == 0 && u.outputTokens > 0 {
		usage.OutputTokens = u.outputTokens
	}
}

// messagesNew is the single entry point for Messages API HTTP calls; records token usage metrics.
func (c *ClaudeProvider) messagesNew(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}
	recordProviderTokenUsage(ctx, c.tel, anthropicUsageInputTokens(msg.Usage), anthropicUsageOutputTokens(msg.Usage))
	return msg, nil
}

// betaMessagesNew is the single entry point for Beta Messages API HTTP calls; records token usage metrics.
func (c *ClaudeProvider) betaMessagesNew(ctx context.Context, params anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	msg, err := c.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}
	recordProviderTokenUsage(ctx, c.tel, anthropicBetaUsageInputTokens(msg.Usage), anthropicBetaUsageOutputTokens(msg.Usage))
	return msg, nil
}

func (c *ClaudeProvider) messagesNewStreaming(
	ctx context.Context,
	params anthropic.MessageNewParams,
	onTextDelta func(delta string),
) (*anthropic.Message, bool, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var (
		finalMsg     anthropic.Message
		deltaEmitted bool
		streamUsage  anthropicStreamUsage
	)
	for stream.Next() {
		ev := stream.Current()
		streamUsage.observe(ev.RawJSON())
		if err := finalMsg.Accumulate(ev); err != nil {
			return nil, deltaEmitted, err
		}
		if handleClaudeTextDeltaEvent(ev, onTextDelta) {
			deltaEmitted = true
		}
	}

	if err := stream.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			streamUsage.apply(&finalMsg.Usage)
			return nil, deltaEmitted, WrapCanceledWithUsage(err, CancelUsage{
				InputTokens:  anthropicUsageInputTokens(finalMsg.Usage),
				OutputTokens: anthropicUsageOutputTokens(finalMsg.Usage),
				Available:    true,
				Source:       "claude_stream_usage",
			})
		}
		return nil, deltaEmitted, err
	}
	if finalMsg.ID == "" {
		return nil, deltaEmitted, fmt.Errorf("stream finished without message_start event")
	}
	streamUsage.apply(&finalMsg.Usage)
	recordProviderTokenUsage(ctx, c.tel, anthropicUsageInputTokens(finalMsg.Usage), anthropicUsageOutputTokens(finalMsg.Usage))
	return &finalMsg, deltaEmitted, nil
}

func (c *ClaudeProvider) betaMessagesNewStreaming(
	ctx context.Context,
	params anthropic.BetaMessageNewParams,
	onTextDelta func(delta string),
) (*anthropic.BetaMessage, bool, error) {
	stream := c.client.Beta.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var (
		finalMsg     anthropic.BetaMessage
		deltaEmitted bool
		streamUsage  anthropicStreamUsage
	)
	for stream.Next() {
		ev := stream.Current()
		streamUsage.observe(ev.RawJSON())
		if err := finalMsg.Accumulate(ev); err != nil {
			return nil, deltaEmitted, err
		}
		if handleClaudeBetaTextDeltaEvent(ev, onTextDelta) {
			deltaEmitted = true
		}
	}

	if err := stream.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			streamUsage.applyBeta(&finalMsg.Usage)
			return nil, deltaEmitted, WrapCanceledWithUsage(err, CancelUsage{
				InputTokens:  anthropicBetaUsageInputTokens(finalMsg.Usage),
				OutputTokens: anthropicBetaUsageOutputTokens(finalMsg.Usage),
				Available:    true,
				Source:       "claude_beta_stream_usage",
			})
		}
		return nil, deltaEmitted, err
	}
	if finalMsg.ID == "" {
		return nil, deltaEmitted, fmt.Errorf("beta stream finished without message_start event")
	}
	streamUsage.applyBeta(&finalMsg.Usage)
	recordProviderTokenUsage(ctx, c.tel, anthropicBetaUsageInputTokens(finalMsg.Usage), anthropicBetaUsageOutputTokens(finalMsg.Usage))
	return &finalMsg, deltaEmitted, nil
}

// Call sends a single Messages API request. Retry handling (429/500/529) is
// delegated to the Anthropic SDK via WithMaxRetries in NewClaudeProvider.
func (c *ClaudeProvider) Call(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return c.messagesNew(ctx, params)
}

// CallBeta sends a single Beta Messages API request.
func (c *ClaudeProvider) CallBeta(ctx context.Context, params anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	return c.betaMessagesNew(ctx, params)
}

// CallWithRetryStreaming calls the streaming Messages API and forwards text deltas to onTextDelta.
// If a retryable error occurs after at least one delta was emitted, it returns immediately to avoid
// duplicate persisted chunks.
func (c *ClaudeProvider) CallWithRetryStreaming(
	ctx context.Context,
	params anthropic.MessageNewParams,
	onTextDelta func(delta string),
) (*anthropic.Message, error) {
	return callClaudeWithRetry(ctx, func(ctx context.Context) (*anthropic.Message, bool, error) {
		return c.messagesNewStreaming(ctx, params, onTextDelta)
	})
}

// CallBetaWithRetryStreaming calls the beta streaming Messages API and forwards text deltas to onTextDelta.
// If a retryable error occurs after at least one delta was emitted, it returns immediately to avoid
// duplicate persisted chunks.
func (c *ClaudeProvider) CallBetaWithRetryStreaming(
	ctx context.Context,
	params anthropic.BetaMessageNewParams,
	onTextDelta func(delta string),
) (*anthropic.BetaMessage, error) {
	return callClaudeWithRetry(ctx, func(ctx context.Context) (*anthropic.BetaMessage, bool, error) {
		return c.betaMessagesNewStreaming(ctx, params, onTextDelta)
	})
}

func callClaudeWithRetry[T any](
	ctx context.Context,
	caller func(context.Context) (*T, bool, error),
) (*T, error) {
	const maxRetries = 3
	rateLimitWaitTimes := []time.Duration{65 * time.Second, 100 * time.Second, 135 * time.Second}
	serverErrorWaitTimes := []time.Duration{5 * time.Second, 30 * time.Second, 60 * time.Second}

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, streamedAnyDelta, err := caller(ctx)
		if err != nil {
			if isRateLimitError(err) {
				if attempt < maxRetries-1 {
					if streamedAnyDelta {
						return nil, err
					}
					if waitErr := waitForRetry(ctx, rateLimitWaitTimes[attempt]); waitErr != nil {
						return nil, waitErr
					}
					continue
				}
			} else if isServerError(err) {
				if attempt < maxRetries-1 {
					if streamedAnyDelta {
						return nil, err
					}
					if waitErr := waitForRetry(ctx, serverErrorWaitTimes[attempt]); waitErr != nil {
						return nil, waitErr
					}
					continue
				}
			}
			return nil, err
		}
		return resp, nil
	}

	return nil, fmt.Errorf("failed after %d attempts due to Anthropic API issues", maxRetries)
}

func handleClaudeTextDeltaEvent(ev anthropic.MessageStreamEventUnion, onTextDelta func(delta string)) bool {
	if onTextDelta == nil {
		return false
	}
	contentDelta, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent)
	if !ok {
		return false
	}
	textDelta, ok := contentDelta.Delta.AsAny().(anthropic.TextDelta)
	if !ok || textDelta.Text == "" {
		return false
	}
	onTextDelta(textDelta.Text)
	return true
}

func handleClaudeBetaTextDeltaEvent(ev anthropic.BetaRawMessageStreamEventUnion, onTextDelta func(delta string)) bool {
	if onTextDelta == nil {
		return false
	}
	contentDelta, ok := ev.AsAny().(anthropic.BetaRawContentBlockDeltaEvent)
	if !ok {
		return false
	}
	textDelta, ok := contentDelta.Delta.AsAny().(anthropic.BetaTextDelta)
	if !ok || textDelta.Text == "" {
		return false
	}
	onTextDelta(textDelta.Text)
	return true
}

// ToGenerateResponse converts an Anthropic Message to the provider-agnostic
// GenerateResponse, extracting text content and token counts.
func (c *ClaudeProvider) ToGenerateResponse(msg *anthropic.Message) *GenerateResponse {
	resp := ClaudeGenerateResponse(
		msg.ID,
		anthropicUsageInputTokens(msg.Usage),
		anthropicUsageOutputTokens(msg.Usage),
		ExtractClaudeText(msg),
	)
	resp.StopReason = string(msg.StopReason)
	return resp
}

// BetaToGenerateResponse converts an Anthropic BetaMessage to the provider-agnostic GenerateResponse.
func (c *ClaudeProvider) BetaToGenerateResponse(msg *anthropic.BetaMessage) *GenerateResponse {
	resp := ClaudeGenerateResponse(
		msg.ID,
		anthropicBetaUsageInputTokens(msg.Usage),
		anthropicBetaUsageOutputTokens(msg.Usage),
		ExtractClaudeBetaText(msg),
	)
	resp.StopReason = string(msg.StopReason)
	return resp
}

// ExtractClaudeText concatenates all text blocks in the response content.
// Currently only returns TextBocks (assistant response), we might want to return tool use or non-text blocks in the future.
func ExtractClaudeText(msg *anthropic.Message) string {
	var parts []string
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok && tb.Text != "" {
			parts = append(parts, tb.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// ExtractClaudeBetaText concatenates all text blocks in a beta response.
func ExtractClaudeBetaText(msg *anthropic.BetaMessage) string {
	if msg == nil {
		return ""
	}
	var parts []string
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.BetaTextBlock); ok && tb.Text != "" {
			parts = append(parts, tb.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// UnmarshalClaudeTextJSON decodes JSON from the assistant text blocks of msg into dst.
//
// With OutputConfig JSON-schema responses, the API still returns structured JSON
// inside standard text content blocks; anthropic.Message does not unmarshal that
// payload into application types for you—only the outer API envelope.
func UnmarshalClaudeTextJSON(msg *anthropic.Message, dst any) error {
	return json.Unmarshal([]byte(ExtractClaudeText(msg)), dst)
}

// CountTokens returns an approximate token count for the given text using the
// shared cl100k_base tokenizer. This is a best-effort approximation for Claude
// (which uses its own tokenizer) but is accurate enough for checkpointing heuristics.
func (c *ClaudeProvider) CountTokens(text string) (int, error) {
	return c.tokenCounter.CountTokens(text)
}

// SelectCarryOverTurns delegates to the shared TokenCounter to select the most
// recent user/assistant turn pairs that fit within maxTokens.
func (c *ClaudeProvider) SelectCarryOverTurns(recent []*models.ChatMessage, maxTurns, maxTokens int) [][2]*models.ChatMessage {
	return c.tokenCounter.SelectCarryOverTurns(recent, maxTurns, maxTokens)
}
