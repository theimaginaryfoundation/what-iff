package provider

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

func TestRecordProviderTokenUsage_nilTelemetry(t *testing.T) {
	t.Parallel()
	ctx := telemetry.WithCallPath(context.Background(), telemetry.CallPathUserChat)
	require.NotPanics(t, func() {
		recordProviderTokenUsage(ctx, nil, 10, 20)
	})
	require.NotPanics(t, func() {
		recordProviderTokenUsage(ctx, &telemetry.Telemetry{}, 10, 20)
	})
}

// recordChatCompletionUsage is the shared entry point used by the DeepSeek/Qwen/
// Mistral/Xiaomi providers (and Gemini). It must tolerate nil responses and nil
// telemetry so a missing usage block never breaks a chat turn.
func TestRecordChatCompletionUsage_NilSafe(t *testing.T) {
	t.Parallel()
	ctx := telemetry.WithCallPath(context.Background(), telemetry.CallPathUserChat)
	require.NotPanics(t, func() {
		recordChatCompletionUsage(ctx, nil, nil)
	})
	require.NotPanics(t, func() {
		recordChatCompletionUsage(ctx, &telemetry.Telemetry{}, &openai.ChatCompletion{})
	})
}
