package provider

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// recordChatCompletionUsage emits provider token-usage metrics for an
// OpenAI-compatible Chat Completions response. Shared by the DeepSeek/Qwen/
// Mistral/Xiaomi providers so their token counts land in telemetry the same way
// GeminiProvider.completionsNew records them. No-op when resp is nil or usage is absent.
func recordChatCompletionUsage(ctx context.Context, tel *telemetry.Telemetry, resp *openai.ChatCompletion) {
	if resp == nil {
		return
	}
	promptTokens, completionTokens := chatCompletionTokenUsage(resp)
	if promptTokens > 0 || completionTokens > 0 {
		recordProviderTokenUsage(ctx, tel, promptTokens, completionTokens)
	}
}

func recordProviderTokenUsage(ctx context.Context, tel *telemetry.Telemetry, inputTokens, outputTokens int64) {
	if tel == nil || tel.Metrics == nil {
		return
	}
	pathStr := string(telemetry.CallPathFromContext(ctx))
	if inputTokens > 0 {
		tel.Metrics.RecordCountHistogram(ctx, telemetry.Tokens, inputTokens, metric.WithAttributes(
			telemetry.InputTokenAttr(),
			attribute.String("token_basis", "provider_actual"),
			attribute.String("call_path", pathStr),
		))
	}
	if outputTokens > 0 {
		tel.Metrics.RecordCountHistogram(ctx, telemetry.Tokens, outputTokens, metric.WithAttributes(
			telemetry.OutputTokenAttr(),
			attribute.String("token_basis", "provider_actual"),
			attribute.String("call_path", pathStr),
		))
	}
}
