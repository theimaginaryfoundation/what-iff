package provider

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
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
	if tel == nil {
		return
	}
	// tel.Metrics may be nil; its methods are no-ops on a nil receiver.
	callPath := telemetry.AttrCallPath.String(string(telemetry.CallPathFromContext(ctx)))
	if inputTokens > 0 {
		tel.Metrics.Record(ctx, telemetry.GenAITokenUsage, float64(inputTokens), telemetry.AttrGenAITokenType.String(telemetry.TokenTypeInput), callPath)
	}
	if outputTokens > 0 {
		tel.Metrics.Record(ctx, telemetry.GenAITokenUsage, float64(outputTokens), telemetry.AttrGenAITokenType.String(telemetry.TokenTypeOutput), callPath)
	}
}
